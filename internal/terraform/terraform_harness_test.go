//go:build invoke_terraform
// +build invoke_terraform

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

// These tests invoke the terraform binary. They require network access in
// order to download providers, and will thus not be run by default.
package terraform

import (
	"context"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/pkg/errors"

	"github.com/crossplane/crossplane-runtime/pkg/test"
)

// Terraform binary to invoke.
var tfBinaryPath = func() string {
	if bin, ok := os.LookupEnv("TF_BINARY"); ok {
		return bin
	}
	return "terraform"
}()

// Terraform test data. We need a fully qualified path because paths are
// relative to the Terraform binary's working directory, not this test file.
var tfTestDataPath = func() string {
	wd, _ := os.Getwd()
	return filepath.Join(wd, "testdata")
}

func TestValidate(t *testing.T) {
	cases := map[string]struct {
		reason string
		module string
		ctx    context.Context
		want   error
	}{
		"ValidModule": {
			reason: "We should not return an error if the module is valid.",
			module: "testdata/validmodule",
			ctx:    context.Background(),
			want:   nil,
		},
		"InvalidModule": {
			reason: "We should return an error if the module is invalid.",
			module: "testdata/invalidmodule",
			ctx:    context.Background(),
			want:   errors.Errorf(errFmtInvalidConfig, 1),
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			// Validation is a read-only operation, so we operate directly on
			// our test data instead of creating a temporary directory.
			tf := Harness{Path: tfBinaryPath, Dir: tc.module}
			got := tf.Validate(tc.ctx)

			if diff := cmp.Diff(tc.want, got, test.EquateErrors()); diff != "" {
				t.Errorf("\n%s\ntf.Validate(...): -want, +got:\n%s", tc.reason, diff)
			}
		})
	}
}

func TestWorkspace(t *testing.T) {
	type args struct {
		ctx  context.Context
		name string
	}

	cases := map[string]struct {
		reason string
		args   args
		want   error
	}{
		"SuccessfulSelect": {
			reason: "It should be possible to select the default workspace, which always exists.",
			args: args{
				ctx:  context.Background(),
				name: "default",
			},
			want: nil,
		},
		"SuccessfulNew": {
			reason: "It should be possible to create a new workspace.",
			args: args{
				ctx:  context.Background(),
				name: "cool",
			},
			want: nil,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			dir, err := ioutil.TempDir("", "provider-terraform-test")
			if err != nil {
				t.Fatalf("Cannot create temporary directory: %v", err)
			}
			defer os.RemoveAll(dir)

			tf := Harness{Path: tfBinaryPath, Dir: dir}
			got := tf.Workspace(tc.args.ctx, tc.args.name)

			if diff := cmp.Diff(tc.want, got, test.EquateErrors()); diff != "" {
				t.Errorf("\n%s\ntf.Workspace(...): -want, +got:\n%s", tc.reason, diff)
			}
		})
	}
}

func TestDeleteWorkspace(t *testing.T) {
	type args struct {
		ctx  context.Context
		name string
	}

	cases := map[string]struct {
		reason string
		args   args
		want   error
	}{
		"SuccessfulDelete": {
			reason: "It should be possible to delete an existing workspace.",
			args: args{
				ctx:  context.Background(),
				name: "cool",
			},
			want: nil,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			dir, err := ioutil.TempDir("", "provider-terraform-test")
			if err != nil {
				t.Fatalf("Cannot create temporary directory: %v", err)
			}
			defer os.RemoveAll(dir)

			tf := Harness{Path: tfBinaryPath, Dir: dir}
			ws := tf.Workspace(tc.args.ctx, tc.args.name)
			got := tf.DeleteCurrentWorkspace(tc.args.ctx)

			if diff := cmp.Diff(tc.want, got, test.EquateErrors()); diff != "" {
				t.Errorf("\n%s\ntf.Workspace(...): -want, +got:\n%s", tc.reason, diff)
			}
		})
	}
}

func TestOutputs(t *testing.T) {
	type want struct {
		outputs []Output
		err     error
	}
	cases := map[string]struct {
		reason string
		module string
		ctx    context.Context
		want   want
	}{
		"ManyOutputs": {
			reason: "We should return outputs from a module.",
			module: "testdata/outputmodule",
			ctx:    context.Background(),
			want: want{
				outputs: []Output{
					{Name: "bool", Type: OutputTypeBool, value: true},
					{Name: "number", Type: OutputTypeNumber, value: float64(42)},
					{
						Name:  "object",
						Type:  OutputTypeObject,
						value: map[string]interface{}{"wow": "suchobject"},
					},
					{Name: "sensitive", Sensitive: true, Type: OutputTypeString, value: "very"},
					{Name: "string", Type: OutputTypeString, value: "very"},
					{
						Name:  "tuple",
						Type:  OutputTypeTuple,
						value: []interface{}{"a", "really", "long", "tuple"},
					},
				},
			},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			// Reading output is a read-only operation, so we operate directly
			// on our test data instead of creating a temporary directory.
			tf := Harness{Path: tfBinaryPath, Dir: tc.module}
			got, err := tf.Outputs(tc.ctx)

			if diff := cmp.Diff(tc.want.outputs, got, cmp.AllowUnexported(Output{})); diff != "" {
				t.Errorf("\n%s\ntf.Outputs(...): -want error, +got error:\n%s", tc.reason, diff)
			}
			if diff := cmp.Diff(tc.want.err, err, test.EquateErrors()); diff != "" {
				t.Errorf("\n%s\ntf.Outputs(...): -want error, +got error:\n%s", tc.reason, diff)
			}
		})
	}
}

func TestResources(t *testing.T) {
	type want struct {
		resources []string
		err       error
	}
	cases := map[string]struct {
		reason string
		module string
		ctx    context.Context
		want   want
	}{
		"ModuleWithResources": {
			reason: "We should return resources from a module.",
			module: "testdata/nullmodule",
			ctx:    context.Background(),
			want: want{
				resources: []string{"null_resource.test", "random_id.test"},
			},
		},
		"ModuleWithoutResources": {
			reason: "We should not return resources from a module when there are none.",
			module: "testdata/outputmodule",
			ctx:    context.Background(),
			want: want{
				resources: []string{},
			},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			// Reading output is a read-only operation, so we operate directly
			// on our test data instead of creating a temporary directory.
			tf := Harness{Path: tfBinaryPath, Dir: tc.module}
			got, err := tf.Resources(tc.ctx)

			if diff := cmp.Diff(tc.want.resources, got, cmp.AllowUnexported(Output{})); diff != "" {
				t.Errorf("\n%s\ntf.Resources(...): -want error, +got error:\n%s", tc.reason, diff)
			}
			if diff := cmp.Diff(tc.want.err, err, test.EquateErrors()); diff != "" {
				t.Errorf("\n%s\ntf.Resources(...): -want error, +got error:\n%s", tc.reason, diff)
			}
		})
	}
}

func TestInitDiffApplyDestroy(t *testing.T) {
	type initArgs struct {
		ctx context.Context
		o   []InitOption
	}
	type args struct {
		ctx context.Context
		o   []Option
	}
	type want struct {
		init    error
		diff    error
		apply   error
		destroy error

		differsBeforeApply bool
		differsAfterApply  bool
	}

	cases := map[string]struct {
		reason      string
		initArgs    initArgs
		diffArgs    args
		applyArgs   args
		destroyArgs args
		want        want
	}{
		"Simple": {
			reason: "It should be possible to initialize, apply, and destroy a simple Terraform module",
			initArgs: initArgs{
				ctx: context.Background(),
				o:   []InitOption{FromModule(filepath.Join(tfTestDataPath(), "nullmodule"))},
			},
			applyArgs: args{
				ctx: context.Background(),
			},
			diffArgs: args{
				ctx: context.Background(),
			},
			destroyArgs: args{
				ctx: context.Background(),
			},
			want: want{
				differsBeforeApply: false,
			},
		},
		"WithVar": {
			reason: "It should be possible to initialize a simple Terraform module, then apply and destroy it with a supplied variable",
			initArgs: initArgs{
				ctx: context.Background(),
				o:   []InitOption{FromModule(filepath.Join(tfTestDataPath(), "nullmodule"))},
			},
			applyArgs: args{
				ctx: context.Background(),
				o:   []Option{WithVar("coolness", "extreme")},
			},
			diffArgs: args{
				ctx: context.Background(),
				o:   []Option{WithVar("coolness", "extreme")},
			},
			destroyArgs: args{
				ctx: context.Background(),
				o:   []Option{WithVar("coolness", "extreme")},
			},
			want: want{
				differsBeforeApply: true,
			},
		},
		"WithHCLVarFile": {
			reason: "It should be possible to initialize a simple Terraform module, then apply and destroy it with a supplied HCL file of variables",
			initArgs: initArgs{
				ctx: context.Background(),
				o:   []InitOption{FromModule(filepath.Join(tfTestDataPath(), "nullmodule"))},
			},
			diffArgs: args{
				ctx: context.Background(),
				o:   []Option{WithVarFile([]byte(`coolness = "extreme!"`), HCL)},
			},
			applyArgs: args{
				ctx: context.Background(),
				o:   []Option{WithVarFile([]byte(`coolness = "extreme!"`), HCL)},
			},
			destroyArgs: args{
				ctx: context.Background(),
				o:   []Option{WithVarFile([]byte(`coolness = "extreme!"`), HCL)},
			},
			want: want{
				differsBeforeApply: true,
			},
		},
		"WithJSONVarFile": {
			reason: "It should be possible to initialize a simple Terraform module, then apply and destroy it with a supplied JSON file of variables",
			initArgs: initArgs{
				ctx: context.Background(),
				o:   []InitOption{FromModule(filepath.Join(tfTestDataPath(), "nullmodule"))},
			},
			diffArgs: args{
				ctx: context.Background(),
				o:   []Option{WithVarFile([]byte(`{"coolness":"extreme!"}`), JSON)},
			},
			applyArgs: args{
				ctx: context.Background(),
				o:   []Option{WithVarFile([]byte(`{"coolness":"extreme!"}`), JSON)},
			},
			destroyArgs: args{
				ctx: context.Background(),
				o:   []Option{WithVarFile([]byte(`{"coolness":"extreme!"}`), JSON)},
			},
			want: want{
				differsBeforeApply: true,
			},
		},
		// NOTE(negz): The goal of these error case tests is to validate that
		// any kind of error classification is happening. We don't want to test
		// too many error cases, because doing so would likely create an overly
		// tight coupling to a particular version of the terraform binary.
		"ModuleNotFound": {
			reason: "Init should return an error when asked to initialize from a module that doesn't exist",
			initArgs: initArgs{
				ctx: context.Background(),
				o:   []InitOption{FromModule("./nonexistent")},
			},
			diffArgs: args{
				ctx: context.Background(),
			},
			applyArgs: args{
				ctx: context.Background(),
			},
			destroyArgs: args{
				ctx: context.Background(),
			},
			want: want{
				init:  errors.New("module not found"),
				diff:  errors.New("no configuration files"),
				apply: errors.New("no configuration files"),
				// Apparently destroy 'works' in this situation ¯\_(ツ)_/¯
			},
		},
		"UndeclaredVar": {
			reason: "Destroy should return an error when supplied a variable not declared by the module",
			initArgs: initArgs{
				ctx: context.Background(),
				o:   []InitOption{FromModule(filepath.Join(tfTestDataPath(), "nullmodule"))},
			},
			diffArgs: args{
				ctx: context.Background(),
			},
			applyArgs: args{
				ctx: context.Background(),
			},
			destroyArgs: args{
				ctx: context.Background(),
				o:   []Option{WithVar("boop", "doop!")},
			},
			want: want{
				destroy: errors.New("value for undeclared variable"),
			},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			// t.Parallel()

			dir, err := ioutil.TempDir("", "provider-terraform-test")
			if err != nil {
				t.Fatalf("Cannot create temporary directory: %v", err)
			}
			defer os.RemoveAll(dir)

			tf := Harness{Path: tfBinaryPath, Dir: dir}

			err = tf.Init(tc.initArgs.ctx, tc.initArgs.o...)
			if diff := cmp.Diff(tc.want.init, err, test.EquateErrors()); diff != "" {
				t.Errorf("\n%s\ntf.Init(...): -want error, +got error:\n%s", tc.reason, diff)
			}

			differs, err := tf.Diff(tc.diffArgs.ctx, tc.diffArgs.o...)
			t.Logf("Want %t, got %t", tc.want.differsBeforeApply, differs)
			if diff := cmp.Diff(tc.want.diff, err, test.EquateErrors()); diff != "" {
				t.Errorf("\n%s\ntf.Diff(...): -want error, +got error (before apply):\n%s", tc.reason, diff)
			}
			if diff := cmp.Diff(tc.want.differsBeforeApply, differs); diff != "" {
				t.Errorf("\n%s\ntf.Diff(...): -want, +got (before apply):\n%s", tc.reason, diff)
			}

			err = tf.Apply(tc.applyArgs.ctx, tc.applyArgs.o...)
			if diff := cmp.Diff(tc.want.apply, err, test.EquateErrors()); diff != "" {
				t.Errorf("\n%s\ntf.Apply(...): -want error, +got error:\n%s", tc.reason, diff)
			}

			differs, err = tf.Diff(tc.diffArgs.ctx, tc.diffArgs.o...)
			if diff := cmp.Diff(tc.want.diff, err, test.EquateErrors()); diff != "" {
				t.Errorf("\n%s\ntf.Diff(...): -want error, +got error (after apply):\n%s", tc.reason, diff)
			}
			if diff := cmp.Diff(tc.want.differsAfterApply, differs); diff != "" {
				t.Errorf("\n%s\ntf.Diff(...): -want, +got (after apply):\n%s", tc.reason, diff)
			}

			err = tf.Destroy(tc.destroyArgs.ctx, tc.destroyArgs.o...)
			if diff := cmp.Diff(tc.want.destroy, err, test.EquateErrors()); diff != "" {
				t.Errorf("\n%s\ntf.Destroy(...): -want error, +got error:\n%s", tc.reason, diff)
			}
		})
	}
}
