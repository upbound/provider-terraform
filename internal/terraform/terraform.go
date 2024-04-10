/*
Copyright 2020 The Crossplane Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Package terraform provides a harness for the Terraform CLI. It is known to
// work with Terraform v0.14.7.
package terraform

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"

	"github.com/crossplane/crossplane-runtime/pkg/logging"
	"github.com/pkg/errors"
)

// Error strings.
const (
	errParse            = "cannot parse Terraform output"
	errWriteVarFile     = "cannot write tfvars file"
	errFmtInvalidConfig = "invalid Terraform configuration: found %d errors"
	errRunCommand       = "shutdown while running terraform command"
	errSigTerm          = "error sending SIGTERM to child process"
	errWaitTerm         = "error waiting for child process to terminate"
	errWriteLogs        = "error writing terraform logs to stdout"

	tfDefault = "default"
)

const varFilePrefix = "crossplane-provider-terraform-"

// Terraform often returns a summary of the error it encountered on a single
// line, prefixed with 'Error: '.
var tfError = regexp.MustCompile(`Error: (.+)\n`)

// Classify errors returned from the Terraform CLI by inspecting its stderr.
func Classify(err error) error {
	ee := &exec.ExitError{}
	if !errors.As(err, &ee) {
		return err
	}

	summary, base64FullErr, err := formatTerraformErrorOutput(string(ee.Stderr))
	if err != nil {
		return err
	}

	formatString := "Terraform encountered an error. Summary: %s. To see the full error run: echo \"%s\" | base64 -d | gunzip"

	return errors.New(fmt.Sprintf(formatString, summary, base64FullErr))
}

// Format Terraform error output as gzipped and base64 encoded string
func formatTerraformErrorOutput(errorOutput string) (string, string, error) {
	// Gzip compress the output and base64 encode it.
	var buffer bytes.Buffer
	gz := gzip.NewWriter(&buffer)

	if _, err := gz.Write([]byte(errorOutput)); err != nil {
		return "", "", err
	}

	if err := gz.Flush(); err != nil {
		return "", "", err
	}

	if err := gz.Close(); err != nil {
		return "", "", err
	}

	if err := gz.Flush(); err != nil {
		return "", "", err
	}

	// Return the first line of the error output as the summary
	var summary string
	lines := strings.Split(errorOutput, "\n")
	if m := tfError.FindSubmatch([]byte(errorOutput)); len(lines) > 0 && len(m) > 1 {
		summary = string(m[1])
	}

	// base64FullErr := base64.StdEncoding.EncodeToString([]byte(errorOutput))
	base64FullErr := base64.StdEncoding.EncodeToString(buffer.Bytes())

	return summary, base64FullErr, nil
}

// NOTE(negz): The gosec linter returns a G204 warning anytime a command is
// executed with any kind of variable input. This isn't inherently a problem,
// and is apparently mostly intended to catch the attention of code auditors per
// https://github.com/securego/gosec/issues/292

// A Harness for running the terraform binary.
type Harness struct {
	// Path to the terraform binary.
	Path string

	// Dir in which to execute the terraform binary.
	Dir string

	// Whether to use the terraform plugin cache
	UsePluginCache bool

	// Whether to enable writing Terraform CLI logs to container stdout
	EnableTerraformCLILogging bool

	// Logger
	Logger logging.Logger

	// Environment Variables
	Envs []string

	// TODO(negz): Harness is a subset of exec.Cmd. If callers need more insight
	// into what the underlying Terraform binary is doing (e.g. for debugging)
	// we could consider allowing them to attach io.Writers to Stdout and Stdin
	// here, like exec.Cmd. Doing so would prevent us from being able to use
	// cmd.Output(), which means we'd have to implement our own version of the
	// logic that copies Stderr into an *exec.ExitError.
}

type initOptions struct {
	args []string
}

// An InitOption affects how a Terraform is initialized.
type InitOption func(o *initOptions)

// FromModule can be used to initialize a Terraform configuration from a module,
// which may be pulled from git, a local directory, a storage bucket, etc.
func FromModule(module string) InitOption {
	return func(o *initOptions) {
		o.args = append(o.args, "-from-module="+module)
	}
}

// WithInitArgs supplies a list of Terraform argument.
func WithInitArgs(v []string) InitOption {
	return func(o *initOptions) {
		o.args = append(o.args, v...)
	}
}

// InitArgsToString converts Terraform init arguments to a list of strings.
func InitArgsToString(o []InitOption) []string {
	io := &initOptions{}
	for _, fn := range o {
		fn(io)
	}
	return io.args
}

// RWMutex protects the terraform shared cache from corruption. If an init is
// performed, it requires a write lock. Only one write lock at a time. If another
// action is performed, a read lock is acquired. More than one read locks can be acquired.
// This prevents an init from inadvertently changing a plugin while it's in use.
// Prevents issues with `text file busy` errors.
var rwmutex = &sync.RWMutex{}

// Init initializes a Terraform configuration.
func (h Harness) Init(ctx context.Context, o ...InitOption) error {
	args := append([]string{"init", "-input=false", "-no-color"}, InitArgsToString(o)...)
	cmd := exec.Command(h.Path, args...) //nolint:gosec
	cmd.Dir = h.Dir
	for _, e := range os.Environ() {
		if strings.Contains(e, "TF_PLUGIN_CACHE_DIR") {
			if !h.UsePluginCache {
				continue
			}
		}
		cmd.Env = append(cmd.Env, e)
	}
	cmd.Env = append(cmd.Env, "TF_CLI_CONFIG_FILE=./.terraformrc")
	if len(h.Envs) > 0 {
		cmd.Env = append(cmd.Env, h.Envs...)
	}

	if h.UsePluginCache {
		rwmutex.Lock()
		defer rwmutex.Unlock()
	}

	_, err := runCommand(ctx, cmd)
	return Classify(err)
}

// Validate a Terraform configuration. Note that there may be interplay between
// validation and initialization. A configuration that needs to be initialized
// but isn't is deemed invalid. Attempts to initialise an invalid configuration
// will result in errors, which are not available in a machine readable format.
func (h Harness) Validate(ctx context.Context) error {
	cmd := exec.Command(h.Path, "validate", "-json") //nolint:gosec
	cmd.Dir = h.Dir
	if len(h.Envs) > 0 {
		cmd.Env = append(os.Environ(), h.Envs...)
	}

	type result struct {
		Valid      bool `json:"valid"`
		ErrorCount int  `json:"error_count"`
	}

	// The validate command returns zero for a valid module and non-zero for an
	// invalid module, but it returns its JSON to stdout either way.
	out, err := runCommand(ctx, cmd)

	r := &result{}
	if jerr := json.Unmarshal(out, r); jerr != nil {
		// If stdout doesn't appear to be the JSON we expected we try to extract
		// an error from stderr.
		if err != nil {
			return Classify(err)
		}
		return errors.Wrap(jerr, errParse)
	}

	if r.Valid {
		return nil
	}

	return errors.Errorf(errFmtInvalidConfig, r.ErrorCount)
}

// Workspace selects the named Terraform workspace. The workspace will be
// created if it does not exist.
func (h Harness) Workspace(ctx context.Context, name string) error {
	cmd := exec.Command(h.Path, "workspace", "select", "-no-color", name) //nolint:gosec
	cmd.Dir = h.Dir
	if len(h.Envs) > 0 {
		cmd.Env = append(os.Environ(), h.Envs...)
	}

	if _, err := runCommand(ctx, cmd); err == nil {
		// We successfully selected the workspace; we're done.
		return nil
	}

	// We weren't able to select a workspace. We assume this was because the
	// workspace doesn't exist, which causes Terraform to return non-zero. This
	// is somewhat optimistic, but it shouldn't hurt to try.
	cmd = exec.Command(h.Path, "workspace", "new", "-no-color", name) //nolint:gosec
	cmd.Dir = h.Dir

	if h.UsePluginCache {
		rwmutex.RLock()
		defer rwmutex.RUnlock()
	}

	_, err := runCommand(ctx, cmd)
	return Classify(err)
}

// DeleteCurrentWorkspace deletes the current Terraform workspace if it is not the default.
func (h Harness) DeleteCurrentWorkspace(ctx context.Context) error {
	cmd := exec.Command(h.Path, "workspace", "show", "-no-color") //nolint:gosec
	cmd.Dir = h.Dir
	if len(h.Envs) > 0 {
		cmd.Env = append(os.Environ(), h.Envs...)
	}

	n, err := runCommand(ctx, cmd)
	if err != nil {
		return Classify(err)
	}
	name := strings.TrimSuffix(string(n), "\n")
	if name == tfDefault {
		return nil
	}

	// Switch to the default workspace
	err = h.Workspace(ctx, tfDefault)
	if err != nil {
		return Classify(err)
	}
	cmd = exec.Command(h.Path, "workspace", "delete", "-no-color", name) //nolint:gosec
	cmd.Dir = h.Dir
	if len(h.Envs) > 0 {
		cmd.Env = append(os.Environ(), h.Envs...)
	}

	if h.UsePluginCache {
		rwmutex.RLock()
		defer rwmutex.RUnlock()
	}

	_, err = runCommand(ctx, cmd)
	if err == nil {
		// We successfully deleted the workspace; we're done.
		return nil
	}
	// TODO(bobh66) The working directory could be deleted here instead of waiting for GC to clean it up
	return Classify(err)
}

// GenerateChecksum calculates the md5sum of the workspace (excluding installed providers) to see if terraform init needs to run
func (h Harness) GenerateChecksum(ctx context.Context) (string, error) {
	command := "/usr/bin/find . -path ./.git -prune -o -path ./.terraform/providers -prune -o -type f -exec /usr/bin/md5sum {} + | LC_ALL=C /usr/bin/sort | /usr/bin/md5sum | /usr/bin/awk '{print $1}'"
	cmd := exec.Command("/bin/sh", "-c", command) //nolint:gosec
	cmd.Dir = h.Dir

	checksum, err := runCommand(ctx, cmd)
	result := strings.ReplaceAll(string(checksum), "\n", "")
	return result, Classify(err)
}

// An OutputType of Terraform.
type OutputType int

// Terraform output types.
const (
	OutputTypeUnknown OutputType = iota
	OutputTypeString
	OutputTypeNumber
	OutputTypeBool
	OutputTypeTuple
	OutputTypeObject
)

func outputType(t string) OutputType {
	switch t {
	case "string":
		return OutputTypeString
	case "number":
		return OutputTypeNumber
	case "bool":
		return OutputTypeBool
	case "tuple":
		return OutputTypeTuple
	case "object":
		return OutputTypeObject
	default:
		return OutputTypeUnknown
	}
}

// An Output from Terraform.
type Output struct {
	Name      string
	Sensitive bool
	Type      OutputType

	value any
}

// Value returns the output's actual value.
func (o Output) Value() any {
	return o.value
}

// StringValue returns the output's value as a string. It should only be used
// for outputs of type 'string'.
func (o Output) StringValue() string {
	if s, ok := o.value.(string); ok {
		return s
	}
	return ""
}

// NumberValue returns the output's value as a number. It should only be used
// for outputs of type 'number'.
func (o Output) NumberValue() float64 {
	if i, ok := o.value.(float64); ok {
		return i
	}
	return 0
}

// BoolValue returns the output's value as a boolean. It should only be used for
// outputs of type 'bool'.
func (o Output) BoolValue() bool {
	if b, ok := o.value.(bool); ok {
		return b
	}
	return false
}

// JSONValue returns the output's value as JSON. It may be used for outputs of
// any type.
func (o Output) JSONValue() ([]byte, error) {
	return json.Marshal(o.value)
}

// Outputs extracts outputs from Terraform state.
func (h Harness) Outputs(ctx context.Context) ([]Output, error) {
	cmd := exec.Command(h.Path, "output", "-json") //nolint:gosec
	cmd.Dir = h.Dir
	if len(h.Envs) > 0 {
		cmd.Env = append(os.Environ(), h.Envs...)
	}

	type output struct {
		Sensitive bool `json:"sensitive"`
		Value     any  `json:"value"`
		Type      any  `json:"type"`
	}

	outputs := map[string]output{}

	if h.UsePluginCache {
		rwmutex.RLock()
		defer rwmutex.RUnlock()
	}

	out, err := runCommand(ctx, cmd)
	if jerr := json.Unmarshal(out, &outputs); jerr != nil {
		// If stdout doesn't appear to be the JSON we expected we try to extract
		// an error from stderr.
		if err != nil {
			return nil, Classify(err)
		}
		return nil, errors.Wrap(jerr, errParse)
	}

	o := make([]Output, 0, len(outputs))
	for name, output := range outputs {
		t := "unknown"

		// the type' field is a string for simple types like 'bool'.
		if s, ok := output.Type.(string); ok {
			t = s
		}

		// The 'type' field is an array whose first element is a string for
		// complex types like 'object'.
		if a, ok := output.Type.([]any); ok && len(a) > 0 {
			if s, ok := a[0].(string); ok {
				t = s
			}
		}

		o = append(o, Output{
			Name:      name,
			Sensitive: output.Sensitive,
			Type:      outputType(t),
			value:     output.Value,
		})
	}

	sort.Slice(o, func(i, j int) bool { return o[i].Name < o[j].Name })
	return o, nil
}

// Resources returns a list of resources in the Terraform state.
func (h Harness) Resources(ctx context.Context) ([]string, error) {
	cmd := exec.Command(h.Path, "state", "list") //nolint:gosec
	cmd.Dir = h.Dir
	if len(h.Envs) > 0 {
		cmd.Env = append(os.Environ(), h.Envs...)
	}

	if h.UsePluginCache {
		rwmutex.RLock()
		defer rwmutex.RUnlock()
	}

	out, err := runCommand(ctx, cmd)
	if err != nil {
		return nil, Classify(err)
	}

	resources := strings.Split(string(out), "\n")
	return resources[:len(resources)-1], nil
}

type varFile struct {
	data     []byte
	filename string
}

type options struct {
	args     []string
	varFiles []varFile
}

// An Option affects how a Terraform is invoked.
type Option func(o *options)

// WithArgs supplies a list of Terraform argument.
func WithArgs(v []string) Option {
	return func(o *options) {
		o.args = append(o.args, v...)
	}
}

// WithVar supplies a Terraform variable.
func WithVar(k, v string) Option {
	return func(o *options) {
		o.args = append(o.args, "-var="+k+"="+v)
	}
}

// The FileFormat of a Terraform file.
type FileFormat int

// Supported Terraform file formats.
const (
	Unknown FileFormat = iota
	HCL
	JSON
)

// WithVarFile supplies a file of Terraform variables.
func WithVarFile(data []byte, f FileFormat) Option {
	return func(o *options) {
		// Terraform uses the file suffix to determine file format.
		filename := varFilePrefix + strconv.Itoa(len(o.varFiles)) + ".tfvars"
		if f == JSON {
			filename += ".json"
		}
		o.args = append(o.args, "-var-file="+filename)
		o.varFiles = append(o.varFiles, varFile{data: data, filename: filename})
	}
}

// Diff invokes 'terraform plan' to determine whether there is a diff between
// the desired and the actual state of the configuration. It returns true if
// there is a diff.
func (h Harness) Diff(ctx context.Context, o ...Option) (bool, error) {
	ao := &options{}
	for _, fn := range o {
		fn(ao)
	}

	for _, vf := range ao.varFiles {
		if err := os.WriteFile(filepath.Join(h.Dir, vf.filename), vf.data, 0600); err != nil {
			return false, errors.Wrap(err, errWriteVarFile)
		}
	}

	args := append([]string{"plan", "-no-color", "-input=false", "-detailed-exitcode", "-lock=false"}, ao.args...)
	cmd := exec.Command(h.Path, args...) //nolint:gosec
	cmd.Dir = h.Dir
	if len(h.Envs) > 0 {
		cmd.Env = append(os.Environ(), h.Envs...)
	}

	// Note: the terraform lock is not used (see the -lock=false flag above) and the rwmutex is
	// intentionally not locked here to avoid excessive blocking. See
	// https://github.com/upbound/provider-terraform/issues/239#issuecomment-1921732682

	// The -detailed-exitcode flag will make terraform plan return:
	// 0 - Succeeded, diff is empty (no changes)
	// 1 - Errored
	// 2 - Succeeded, there is a diff
	log, err := runCommand(ctx, cmd)
	switch cmd.ProcessState.ExitCode() {
	case 1:
		ee := &exec.ExitError{}
		errors.As(err, &ee)
		if h.EnableTerraformCLILogging {
			h.Logger.Info(string(ee.Stderr))
		}
	case 2:
		if h.EnableTerraformCLILogging {
			h.Logger.Info(string(log))
		}
		return true, nil
	}
	return false, Classify(err)
}

// Apply a Terraform configuration.
func (h Harness) Apply(ctx context.Context, o ...Option) error {
	ao := &options{}
	for _, fn := range o {
		fn(ao)
	}

	for _, vf := range ao.varFiles {
		if err := os.WriteFile(filepath.Join(h.Dir, vf.filename), vf.data, 0600); err != nil {
			return errors.Wrap(err, errWriteVarFile)
		}
	}

	args := append([]string{"apply", "-no-color", "-auto-approve", "-input=false"}, ao.args...)
	cmd := exec.Command(h.Path, args...) //nolint:gosec
	cmd.Dir = h.Dir
	if len(h.Envs) > 0 {
		cmd.Env = append(os.Environ(), h.Envs...)
	}

	if h.UsePluginCache {
		rwmutex.RLock()
		defer rwmutex.RUnlock()
	}

	// In case of terraform apply
	// 0 - Succeeded
	// Non Zero output - Errored

	log, err := runCommand(ctx, cmd)
	switch cmd.ProcessState.ExitCode() {
	case 0:
		if h.EnableTerraformCLILogging {
			h.Logger.Info(string(log))
		}
	default:
		ee := &exec.ExitError{}
		errors.As(err, &ee)
		if h.EnableTerraformCLILogging {
			h.Logger.Info(string(ee.Stderr))
		}
	}
	return Classify(err)
}

// Destroy a Terraform configuration.
func (h Harness) Destroy(ctx context.Context, o ...Option) error {
	do := &options{}
	for _, fn := range o {
		fn(do)
	}

	for _, vf := range do.varFiles {
		if err := os.WriteFile(filepath.Join(h.Dir, vf.filename), vf.data, 0600); err != nil {
			return errors.Wrap(err, errWriteVarFile)
		}
	}

	args := append([]string{"destroy", "-no-color", "-auto-approve", "-input=false"}, do.args...)
	cmd := exec.Command(h.Path, args...) //nolint:gosec
	cmd.Dir = h.Dir
	if len(h.Envs) > 0 {
		cmd.Env = append(os.Environ(), h.Envs...)
	}

	if h.UsePluginCache {
		rwmutex.RLock()
		defer rwmutex.RUnlock()
	}

	log, err := runCommand(ctx, cmd)

	// In case of terraform destroy
	// 0 - Succeeded
	// Non Zero output - Errored
	switch cmd.ProcessState.ExitCode() {
	case 0:
		if h.EnableTerraformCLILogging {
			h.Logger.Info(string(log))
		}
	default:
		ee := &exec.ExitError{}
		errors.As(err, &ee)
		if h.EnableTerraformCLILogging {
			h.Logger.Info(string(ee.Stderr))
		}
	}
	return Classify(err)
}

// cmdResult represents the result of the command execution
type cmdResult struct {
	out []byte
	err error
}

// runCommand executes the requested command and sends the process SIGTERM if the context finishes before the command
func runCommand(ctx context.Context, c *exec.Cmd) ([]byte, error) {
	ch := make(chan cmdResult, 1)
	go func() {
		defer close(ch)
		r, e := c.Output()
		ch <- cmdResult{out: r, err: e}
	}()
	select {
	case <-ctx.Done():
		err := ctx.Err()
		// This could be container termination or the reconciliation deadline was exceeded.  Either way send a
		// SIGTERM to the running process and wait for either the command to finish or the process to get killed.
		e := c.Process.Signal(syscall.SIGTERM)
		if e != nil {
			return nil, errors.Wrap(errors.Wrap(err, errRunCommand), errors.Wrap(e, errSigTerm).Error())
		}
		e = c.Wait()
		if e != nil {
			return nil, errors.Wrap(errors.Wrap(err, errRunCommand), errors.Wrap(e, errWaitTerm).Error())
		}
		return nil, errors.Wrap(err, errRunCommand)
	case res := <-ch:
		return res.out, res.err
	}
}
