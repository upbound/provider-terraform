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
	"testing"

	"github.com/google/go-cmp/cmp"
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
			o: Output{value: []interface{}{"imastring", 42, true}},
			want: want{
				j: []byte(`["imastring",42,true]`),
			},
		},
		"ValueIsObject": {
			o: Output{value: map[string]interface{}{
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
