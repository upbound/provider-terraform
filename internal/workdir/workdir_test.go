/*
Copyright 2021 The Crossplane Authors.

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

package workdir

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/pkg/errors"
	"github.com/spf13/afero"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/crossplane/crossplane-runtime/pkg/test"

	"github.com/crossplane-contrib/provider-terraform/apis/v1alpha1"
)

func withDirs(fs afero.Afero, dir ...string) afero.Afero {
	for _, d := range dir {
		_ = fs.Mkdir(d, os.ModePerm)
	}
	return fs
}

func getDirs(fs afero.Afero, parentDir string) []string {
	dirs := make([]string, 0)
	fis, _ := fs.ReadDir(parentDir)
	for _, fi := range fis {
		if !fi.IsDir() {
			continue
		}
		dirs = append(dirs, fi.Name())
	}
	return dirs
}

func TestCollect(t *testing.T) {
	parentDir := "/test"

	type fields struct {
		kube       client.Client
		parentdDir string
		fs         afero.Afero
	}
	type args struct {
		ctx context.Context
	}
	type want struct {
		dirs []string
		err  error
	}
	cases := map[string]struct {
		reason string
		fields fields
		args   args
		want   want
	}{
		"ErrNoParentDir": {
			reason: "Garbage collection should fail when the parent directory does not exist.",
			fields: fields{
				kube:       &test.MockClient{MockList: test.NewMockListFn(nil)},
				parentdDir: parentDir,
				fs:         afero.Afero{Fs: afero.NewMemMapFs()},
			},
			want: want{
				err: errors.Wrapf(errors.Errorf("open %s: file does not exist", parentDir), errFmtReadDir, parentDir),
			},
		},
		"NoOp": {
			reason: "Garbage collection should succeed when there are no workspaces or workdirs.",
			fields: fields{
				kube:       &test.MockClient{MockList: test.NewMockListFn(nil)},
				parentdDir: parentDir,
				fs:         withDirs(afero.Afero{Fs: afero.NewMemMapFs()}, parentDir),
			},
			want: want{
				err: nil,
			},
		},
		"Success": {
			reason: "Workdirs belonging to workspaces that no longer exist should be successfully garbage collected.",
			fields: fields{
				kube: &test.MockClient{MockList: test.NewMockListFn(nil, func(obj client.ObjectList) error {
					*obj.(*v1alpha1.WorkspaceList) = v1alpha1.WorkspaceList{Items: []v1alpha1.Workspace{
						{ObjectMeta: metav1.ObjectMeta{UID: types.UID("8371dd9e-dd3f-4a42-bd8c-340c4744f6de")}},
						{ObjectMeta: metav1.ObjectMeta{UID: types.UID("ebaac629-43a3-4b39-8138-d7ac19cafe11")}},
					}}
					return nil
				})},
				parentdDir: parentDir,
				fs: withDirs(afero.Afero{Fs: afero.NewMemMapFs()},
					parentDir,
					filepath.Join(parentDir, "8371dd9e-dd3f-4a42-bd8c-340c4744f6de"),
					filepath.Join(parentDir, "ebaac629-43a3-4b39-8138-d7ac19cafe11"),
					filepath.Join(parentDir, "0d177133-1a2f-4ce2-93d2-f8212d3344e7"),
					filepath.Join(parentDir, "helm"),
					filepath.Join(parentDir, "registry.terraform.io"),
				),
			},
			want: want{
				dirs: []string{"8371dd9e-dd3f-4a42-bd8c-340c4744f6de", "ebaac629-43a3-4b39-8138-d7ac19cafe11", "helm", "registry.terraform.io"},
			},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			gc := NewGarbageCollector(tc.fields.kube, tc.fields.parentdDir, WithFs(tc.fields.fs))
			err := gc.collect(tc.args.ctx)
			if diff := cmp.Diff(tc.want.err, err, test.EquateErrors()); diff != "" {
				t.Errorf("gc.collect(...): -want error, +got error:\n%s", diff)
			}

			got := getDirs(tc.fields.fs, tc.fields.parentdDir)
			if diff := cmp.Diff(tc.want.dirs, got, cmpopts.EquateEmpty()); diff != "" {
				t.Errorf("gc.collect(...): -want dirs, +got dirs:\n%s", diff)
			}
		})
	}

}
