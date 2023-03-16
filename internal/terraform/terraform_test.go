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

package terraform

import (
	"os/exec"
	"testing"

	"github.com/MakeNowJust/heredoc"
	"github.com/google/go-cmp/cmp"
	"github.com/pkg/errors"
)

func TestOutputStringValue(t *testing.T) {
	cases := map[string]struct {
		o    Output
		want string
	}{
		"ValueIsString": {
			o:    Output{value: "imastring!"},
			want: "imastring!",
		},
		"ValueIsNotString": {
			o:    Output{value: 42},
			want: "",
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := tc.o.StringValue()
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("\no.StringValue(): -want, +got:\n%s", diff)
			}
		})
	}
}

func TestOutputNumberValue(t *testing.T) {
	cases := map[string]struct {
		o    Output
		want float64
	}{
		"ValueIsFloat": {
			o:    Output{value: float64(42.0)},
			want: 42.0,
		},
		// We create outputs by decoding from JSON, so numbers should always be
		// a float64.
		"ValueIsNotFloat": {
			o:    Output{value: 42},
			want: 0,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := tc.o.NumberValue()
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("\no.NumberValue(): -want, +got:\n%s", diff)
			}
		})
	}
}

func TestOutputBoolValue(t *testing.T) {
	cases := map[string]struct {
		o    Output
		want bool
	}{
		"ValueIsBool": {
			o:    Output{value: true},
			want: true,
		},
		"ValueIsNotBool": {
			o:    Output{value: "DEFINITELY!"},
			want: false,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := tc.o.BoolValue()
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("\no.BoolValue(): -want, +got:\n%s", diff)
			}
		})
	}
}

func TestOutputJSONValue(t *testing.T) {
	type want struct {
		j   []byte
		err error
	}
	cases := map[string]struct {
		o    Output
		want want
	}{
		"ValueIsString": {
			o: Output{value: "imastring!"},
			want: want{
				j: []byte(`"imastring!"`),
			},
		},
		"ValueIsNumber": {
			o: Output{value: 42.0},
			want: want{
				j: []byte(`42`),
			},
		},
		"ValueIsBool": {
			o: Output{value: true},
			want: want{
				j: []byte(`true`),
			},
		},
		"ValueIsTuple": {
			o: Output{value: []any{"imastring", 42, true}},
			want: want{
				j: []byte(`["imastring",42,true]`),
			},
		},
		"ValueIsObject": {
			o: Output{value: map[string]any{
				"cool": 42,
			}},
			want: want{
				j: []byte(`{"cool":42}`),
			},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := tc.o.JSONValue()
			if diff := cmp.Diff(tc.want.err, err); diff != "" {
				t.Errorf("\no.JSONValue(): -want error, +got error:\n%s", diff)
			}
			if diff := cmp.Diff(tc.want.j, got); diff != "" {
				t.Errorf("\no.JSONValue(): -want, +got:\n%s", diff)
			}
		})
	}
}

func TestClassify(t *testing.T) {
	tferrs := make(map[string]error)
	expectedOutput := make(map[string]error)

	tferrs["unexpectedName"] = &exec.ExitError{
		Stderr: []byte(heredoc.Doc(`
	│ Error: Unsupported argument
	│
	│   on test.tf line 10, in resource "aws_s3_bucket" "example":
	│   10:   name = "cp-example-${terraform.workspace}-${random_id.example.hex}"
	│
	│ An argument named "name" is not expected here.
	`)),
	}

	expectedOutput["unexpectedName"] = errors.New(
		heredoc.Doc(
			`Terraform encountered an error. Summary: Unsupported argument. To see the full error run: echo "H4sIAAAAAAAA/zyPMWoDMRBFe5/iI1xmhU26hRQpcoTUi6L9jhdbIzEa4QXjxmfwCX2SsGFxMUzzeLz/fNzxpZq1x7fUVkpW44igvy1RbPN83JcDkAXGat4OOE9C7HdvmATKmptGwoVLHer78NPiiebgOIdUznT9KtjvegASEvEBF0u3At32alQNh6zJX7KeagmRt2571SBjTsM0+hX1R84394r6lFfov3eEW57DVCHZwLkwLnOOVPrNHwAAAP//AQAA//+ac3Cu7AAAAA==" | base64 -d | gunzip`,
		),
	)

	tferrs["tooManyListItems"] = &exec.ExitError{
		Stderr: []byte(heredoc.Doc(`
	│ Error: Too many list items
	│
	│   with aws_cognito_user_pool_client.client,
	│   on main.tf line 21, in resource "aws_cognito_user_pool_client" "client":
	│   21:   allowed_oauth_flows = jsondecode(base64decode("ICBbIkFMTE9XX0FETUlOX1VTRVJfUEFTU1dPUkRfQVVUSCIsICJBTExPV19SRUZSRVNIX1RPS0VOX0FVVEgiLCAiQUxMT1dfVVNFUl9QQVNTV09SRF9BVVRIIiwgIkFMTE9XX1VTRVJfU1JQX0FVVEgiXQo="))
	│
	│ Attribute allowed_oauth_flows supports 3 item maximum, but config has 4 declared.
	`)),
	}

	expectedOutput["tooManyListItems"] = errors.New(
		heredoc.Doc(
			`Terraform encountered an error. Summary: Too many list items. To see the full error run: echo "H4sIAAAAAAAA/3yRwWrbQBBA7/mKQacGjNGmoSBDDrGQQKZxLGl3Eb2IlbSSt1ntmN0Vcq/5hnyhv6S0lX0qOQwzA8N7zMzl4x0Sa9FugCLCKMwv0Mp5UF6O7u7y8f4nAGBW/ghidnWLg1Ee68lJW58Qdd1qJY1f/0urZR4NjEKZte9BKyPhgaxAGbDS4WRbCcFnrACCpdgsuAeyAQChNc6yq1FM/lj3GmcHT/DToelki5380ggnvz0uTZDF2yZ7S19oElVVmCaU6deKcFrwXc+SlDLSHdhb0eecszLOXBbvtjQ5HziJyoL9KAu+zypSHMqQv1ZhynkyqO/xs8rZ+YWSrud8nzId5TnfUx5GZZFGW86LLFPzcPNefWSXXxlVjk/B/f3tus/eW9VMXv53QTedTmi9g69/nwKjOKtxGlfQTB5aNL0a4CgcPEInWy2s7NZ3vwEAAP//AQAA//9AvYb+1wEAAA==" | base64 -d | gunzip`,
		),
	)

	output := Classify(tferrs["unexpectedName"])

	if output.Error() != expectedOutput["unexpectedName"].Error() {
		t.Errorf("Unexpected error classification got:\n`%s`\nexpected:\n`%s`", output, expectedOutput["unexpectedName"])
	}

	output = Classify(tferrs["tooManyListItems"])

	if output.Error() != expectedOutput["tooManyListItems"].Error() {
		t.Errorf("Unexpected error classification got:\n`%s`\nexpected:\n`%s`", output, expectedOutput["tooManyListItems"])
	}
}

func TestFormatTerraformErrorOutput(t *testing.T) {
	tferrs := make(map[string]string)
	expectedOutput := make(map[string]map[string]string)

	tferrs["unexpectedName"] = heredoc.Doc(`
	│ Error: Unsupported argument
	│
	│   on test.tf line 10, in resource "aws_s3_bucket" "example":
	│   10:   name = "cp-example-${terraform.workspace}-${random_id.example.hex}"
	│
	│ An argument named "name" is not expected here.
	`)

	expectedOutput["unexpectedName"] = make(map[string]string)
	expectedOutput["unexpectedName"]["summary"] = heredoc.Doc(`
	Unsupported argument`)

	expectedOutput["unexpectedName"]["base64full"] = "H4sIAAAAAAAA/zyPMWoDMRBFe5/iI1xmhU26hRQpcoTUi6L9jhdbIzEa4QXjxmfwCX2SsGFxMUzzeLz/fNzxpZq1x7fUVkpW44igvy1RbPN83JcDkAXGat4OOE9C7HdvmATKmptGwoVLHer78NPiiebgOIdUznT9KtjvegASEvEBF0u3At32alQNh6zJX7KeagmRt2571SBjTsM0+hX1R84394r6lFfov3eEW57DVCHZwLkwLnOOVPrNHwAAAP//AQAA//+ac3Cu7AAAAA=="

	tferrs["tooManyListItems"] = heredoc.Doc(`
	│ Error: Too many list items
	│
	│   with aws_cognito_user_pool_client.client,
	│   on main.tf line 21, in resource "aws_cognito_user_pool_client" "client":
	│   21:   allowed_oauth_flows = jsondecode(base64decode("ICBbIkFMTE9XX0FETUlOX1VTRVJfUEFTU1dPUkRfQVVUSCIsICJBTExPV19SRUZSRVNIX1RPS0VOX0FVVEgiLCAiQUxMT1dfVVNFUl9QQVNTV09SRF9BVVRIIiwgIkFMTE9XX1VTRVJfU1JQX0FVVEgiXQo="))
	│
	│ Attribute allowed_oauth_flows supports 3 item maximum, but config has 4 declared.
	`)

	expectedOutput["tooManyListItems"] = make(map[string]string)
	expectedOutput["tooManyListItems"]["summary"] = heredoc.Doc(`
	Too many list items`)

	expectedOutput["tooManyListItems"]["base64full"] = "H4sIAAAAAAAA/3yRwWrbQBBA7/mKQacGjNGmoSBDDrGQQKZxLGl3Eb2IlbSSt1ntmN0Vcq/5hnyhv6S0lX0qOQwzA8N7zMzl4x0Sa9FugCLCKMwv0Mp5UF6O7u7y8f4nAGBW/ghidnWLg1Ee68lJW58Qdd1qJY1f/0urZR4NjEKZte9BKyPhgaxAGbDS4WRbCcFnrACCpdgsuAeyAQChNc6yq1FM/lj3GmcHT/DToelki5380ggnvz0uTZDF2yZ7S19oElVVmCaU6deKcFrwXc+SlDLSHdhb0eecszLOXBbvtjQ5HziJyoL9KAu+zypSHMqQv1ZhynkyqO/xs8rZ+YWSrud8nzId5TnfUx5GZZFGW86LLFPzcPNefWSXXxlVjk/B/f3tus/eW9VMXv53QTedTmi9g69/nwKjOKtxGlfQTB5aNL0a4CgcPEInWy2s7NZ3vwEAAP//AQAA//9AvYb+1wEAAA=="

	summary, base64FullErr, err := formatTerraformErrorOutput(tferrs["unexpectedName"])
	if err != nil {
		t.Errorf("Unexpected error: %s", err)
	}

	if summary != expectedOutput["unexpectedName"]["summary"] {
		t.Errorf(
			"Unexpected error summary value got:`%s`\nexpected: `%s`",
			summary,
			expectedOutput["unexpectedName"]["summary"],
		)
	}

	if base64FullErr != expectedOutput["unexpectedName"]["base64full"] {
		t.Errorf(
			"Unexpected error base64full got:`%s`\nexpected: `%s`",
			base64FullErr,
			expectedOutput["unexpectedName"]["base64full"],
		)
	}

	summary, base64FullErr, err = formatTerraformErrorOutput(tferrs["tooManyListItems"])

	if err != nil {
		t.Errorf("Unexpected error: %s", err)
	}

	if summary != expectedOutput["tooManyListItems"]["summary"] {
		t.Errorf(
			"Unexpected error classification got:`%s`\nexpected: `%s`",
			summary,
			expectedOutput["tooManyListItems"]["summary"],
		)
	}

	if base64FullErr != expectedOutput["tooManyListItems"]["base64full"] {
		t.Errorf(
			"Unexpected error base64full got:`%s`\nexpected: `%s`",
			base64FullErr,
			expectedOutput["tooManyListItems"]["base64full"],
		)
	}
}
