// Copyright © 2017 Aqua Security Software Ltd. <info@aquasec.com>
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package cmd

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"testing"

	"github.com/spf13/viper"
)

var g string
var e []error
var eIndex int

func fakeps(proc string) string {
	return g
}

func fakestat(file string) (os.FileInfo, error) {
	err := e[eIndex]
	eIndex++
	return nil, err
}

func TestVerifyBin(t *testing.T) {
	cases := []struct {
		proc  string
		psOut string
		exp   bool
	}{
		{proc: "single", psOut: "single", exp: true},
		{proc: "single", psOut: "", exp: false},
		{proc: "two words", psOut: "two words", exp: true},
		{proc: "two words", psOut: "", exp: false},
		{proc: "cmd", psOut: "cmd param1 param2", exp: true},
		{proc: "cmd param", psOut: "cmd param1 param2", exp: true},
		{proc: "cmd param", psOut: "cmd", exp: false},
		{proc: "cmd", psOut: "cmd x \ncmd y", exp: true},
		{proc: "cmd y", psOut: "cmd x \ncmd y", exp: true},
		{proc: "cmd", psOut: "/usr/bin/cmd", exp: true},
		{proc: "cmd", psOut: "kube-cmd", exp: false},
		{proc: "cmd", psOut: "/usr/bin/kube-cmd", exp: false},
	}

	psFunc = fakeps
	for id, c := range cases {
		t.Run(strconv.Itoa(id), func(t *testing.T) {
			g = c.psOut
			v := verifyBin(c.proc)
			if v != c.exp {
				t.Fatalf("Expected %v got %v", c.exp, v)
			}
		})
	}
}

func TestFindExecutable(t *testing.T) {
	cases := []struct {
		candidates []string // list of executables we'd consider
		psOut      string   // fake output from ps
		exp        string   // the one we expect to find in the (fake) ps output
		expErr     bool
	}{
		{candidates: []string{"one", "two", "three"}, psOut: "two", exp: "two"},
		{candidates: []string{"one", "two", "three"}, psOut: "two three", exp: "two"},
		{candidates: []string{"one double", "two double", "three double"}, psOut: "two double is running", exp: "two double"},
		{candidates: []string{"one", "two", "three"}, psOut: "blah", expErr: true},
		{candidates: []string{"one double", "two double", "three double"}, psOut: "two", expErr: true},
		{candidates: []string{"apiserver", "kube-apiserver"}, psOut: "kube-apiserver", exp: "kube-apiserver"},
		{candidates: []string{"apiserver", "kube-apiserver", "hyperkube-apiserver"}, psOut: "kube-apiserver", exp: "kube-apiserver"},
	}

	psFunc = fakeps
	for id, c := range cases {
		t.Run(strconv.Itoa(id), func(t *testing.T) {
			g = c.psOut
			e, err := findExecutable(c.candidates)
			if e != c.exp {
				t.Fatalf("Expected %v got %v", c.exp, e)
			}

			if err == nil && c.expErr {
				t.Fatalf("Expected error")
			}

			if err != nil && !c.expErr {
				t.Fatalf("Didn't expect error: %v", err)
			}
		})
	}
}

func TestGetBinaries(t *testing.T) {
	cases := []struct {
		config    map[string]interface{}
		psOut     string
		exp       map[string]string
		expectErr bool
	}{
		{
			config:    map[string]interface{}{"components": []string{"apiserver"}, "apiserver": map[string]interface{}{"bins": []string{"apiserver", "kube-apiserver"}}},
			psOut:     "kube-apiserver",
			exp:       map[string]string{"apiserver": "kube-apiserver"},
			expectErr: false,
		},
		{
			// "thing" is not in the list of components
			config:    map[string]interface{}{"components": []string{"apiserver"}, "apiserver": map[string]interface{}{"bins": []string{"apiserver", "kube-apiserver"}}, "thing": map[string]interface{}{"bins": []string{"something else", "thing"}}},
			psOut:     "kube-apiserver thing",
			exp:       map[string]string{"apiserver": "kube-apiserver"},
			expectErr: false,
		},
		{
			// "anotherthing" in list of components but doesn't have a defintion
			config:    map[string]interface{}{"components": []string{"apiserver", "anotherthing"}, "apiserver": map[string]interface{}{"bins": []string{"apiserver", "kube-apiserver"}}, "thing": map[string]interface{}{"bins": []string{"something else", "thing"}}},
			psOut:     "kube-apiserver thing",
			exp:       map[string]string{"apiserver": "kube-apiserver"},
			expectErr: false,
		},
		{
			// more than one component
			config:    map[string]interface{}{"components": []string{"apiserver", "thing"}, "apiserver": map[string]interface{}{"bins": []string{"apiserver", "kube-apiserver"}}, "thing": map[string]interface{}{"bins": []string{"something else", "thing"}}},
			psOut:     "kube-apiserver \nthing",
			exp:       map[string]string{"apiserver": "kube-apiserver", "thing": "thing"},
			expectErr: false,
		},
		{
			// default binary to component name
			config:    map[string]interface{}{"components": []string{"apiserver", "thing"}, "apiserver": map[string]interface{}{"bins": []string{"apiserver", "kube-apiserver"}}, "thing": map[string]interface{}{"bins": []string{"something else", "thing"}, "optional": true}},
			psOut:     "kube-apiserver \notherthing some params",
			exp:       map[string]string{"apiserver": "kube-apiserver", "thing": "thing"},
			expectErr: false,
		},
		{
			// missing mandatory component
			config:    map[string]interface{}{"components": []string{"apiserver", "thing"}, "apiserver": map[string]interface{}{"bins": []string{"apiserver", "kube-apiserver"}}, "thing": map[string]interface{}{"bins": []string{"something else", "thing"}, "optional": true}},
			psOut:     "otherthing some params",
			exp:       map[string]string{"apiserver": "kube-apiserver", "thing": "thing"},
			expectErr: true,
		},
	}

	v := viper.New()
	psFunc = fakeps

	for id, c := range cases {
		t.Run(strconv.Itoa(id), func(t *testing.T) {
			g = c.psOut
			for k, val := range c.config {
				v.Set(k, val)
			}
			m, err := getBinaries(v)
			if c.expectErr {
				if err == nil {
					t.Fatal("Got nil Expected error")
				}
			} else if !reflect.DeepEqual(m, c.exp) {
				t.Fatalf("Got %v\nExpected %v", m, c.exp)
			}
		})
	}
}

func TestMultiWordReplace(t *testing.T) {
	cases := []struct {
		input   string
		sub     string
		subname string
		output  string
	}{
		{input: "Here's a file with no substitutions", sub: "blah", subname: "blah", output: "Here's a file with no substitutions"},
		{input: "Here's a file with a substitution", sub: "blah", subname: "substitution", output: "Here's a file with a blah"},
		{input: "Here's a file with multi-word substitutions", sub: "multi word", subname: "multi-word", output: "Here's a file with 'multi word' substitutions"},
		{input: "Here's a file with several several substitutions several", sub: "blah", subname: "several", output: "Here's a file with blah blah substitutions blah"},
	}
	for id, c := range cases {
		t.Run(strconv.Itoa(id), func(t *testing.T) {
			s := multiWordReplace(c.input, c.subname, c.sub)
			if s != c.output {
				t.Fatalf("Expected %s got %s", c.output, s)
			}
		})
	}
}

func TestKubeVersionRegex(t *testing.T) {
	ver := getVersionFromKubectlOutput(`Client Version: v1.8.0
		Server Version: v1.8.12
		`)
	if ver != "1.8" {
		t.Fatalf("Expected 1.8 got %s", ver)
	}

	ver = getVersionFromKubectlOutput("Something completely different")
	if ver != "1.6" {
		t.Fatalf("Expected 1.6 got %s", ver)
	}
}

func TestFindConfigFile(t *testing.T) {
	cases := []struct {
		input       []string
		statResults []error
		exp         string
	}{
		{input: []string{"myfile"}, statResults: []error{nil}, exp: "myfile"},
		{input: []string{"thisfile", "thatfile"}, statResults: []error{os.ErrNotExist, nil}, exp: "thatfile"},
		{input: []string{"thisfile", "thatfile"}, statResults: []error{os.ErrNotExist, os.ErrNotExist}, exp: ""},
	}

	statFunc = fakestat
	for id, c := range cases {
		t.Run(strconv.Itoa(id), func(t *testing.T) {
			e = c.statResults
			eIndex = 0
			conf := findConfigFile(c.input)
			if conf != c.exp {
				t.Fatalf("Got %s expected %s", conf, c.exp)
			}
		})
	}
}

func TestGetConfigFiles(t *testing.T) {
	cases := []struct {
		config      map[string]interface{}
		exp         map[string]string
		statResults []error
	}{
		{
			config:      map[string]interface{}{"components": []string{"apiserver"}, "apiserver": map[string]interface{}{"confs": []string{"apiserver", "kube-apiserver"}}},
			statResults: []error{os.ErrNotExist, nil},
			exp:         map[string]string{"apiserver": "kube-apiserver"},
		},
		{
			// Component "thing" isn't included in the list of components
			config: map[string]interface{}{
				"components": []string{"apiserver"},
				"apiserver":  map[string]interface{}{"confs": []string{"apiserver", "kube-apiserver"}},
				"thing":      map[string]interface{}{"confs": []string{"/my/file/thing"}}},
			statResults: []error{os.ErrNotExist, nil},
			exp:         map[string]string{"apiserver": "kube-apiserver"},
		},
		{
			// More than one component
			config: map[string]interface{}{
				"components": []string{"apiserver", "thing"},
				"apiserver":  map[string]interface{}{"confs": []string{"apiserver", "kube-apiserver"}},
				"thing":      map[string]interface{}{"confs": []string{"/my/file/thing"}}},
			statResults: []error{os.ErrNotExist, nil, nil},
			exp:         map[string]string{"apiserver": "kube-apiserver", "thing": "/my/file/thing"},
		},
		{
			// Default thing to specified default config
			config: map[string]interface{}{
				"components": []string{"apiserver", "thing"},
				"apiserver":  map[string]interface{}{"confs": []string{"apiserver", "kube-apiserver"}},
				"thing":      map[string]interface{}{"confs": []string{"/my/file/thing"}, "defaultconf": "another/thing"}},
			statResults: []error{os.ErrNotExist, nil, os.ErrNotExist},
			exp:         map[string]string{"apiserver": "kube-apiserver", "thing": "another/thing"},
		},
		{
			// Default thing to component name
			config: map[string]interface{}{
				"components": []string{"apiserver", "thing"},
				"apiserver":  map[string]interface{}{"confs": []string{"apiserver", "kube-apiserver"}},
				"thing":      map[string]interface{}{"confs": []string{"/my/file/thing"}}},
			statResults: []error{os.ErrNotExist, nil, os.ErrNotExist},
			exp:         map[string]string{"apiserver": "kube-apiserver", "thing": "thing"},
		},
	}

	v := viper.New()
	statFunc = fakestat

	for id, c := range cases {
		t.Run(strconv.Itoa(id), func(t *testing.T) {
			for k, val := range c.config {
				v.Set(k, val)
			}
			e = c.statResults
			eIndex = 0

			m := getFiles(v, "config")
			if !reflect.DeepEqual(m, c.exp) {
				t.Fatalf("Got %v\nExpected %v", m, c.exp)
			}
		})
	}
}

func TestGetServiceFiles(t *testing.T) {
	cases := []struct {
		config      map[string]interface{}
		exp         map[string]string
		statResults []error
	}{
		{
			config: map[string]interface{}{
				"components": []string{"kubelet"},
				"kubelet":    map[string]interface{}{"svc": []string{"kubelet", "10-kubeadm.conf"}},
			},
			statResults: []error{os.ErrNotExist, nil},
			exp:         map[string]string{"kubelet": "10-kubeadm.conf"},
		},
		{
			// Component "thing" isn't included in the list of components
			config: map[string]interface{}{
				"components": []string{"kubelet"},
				"kubelet":    map[string]interface{}{"svc": []string{"kubelet", "10-kubeadm.conf"}},
				"thing":      map[string]interface{}{"svc": []string{"/my/file/thing"}},
			},
			statResults: []error{os.ErrNotExist, nil},
			exp:         map[string]string{"kubelet": "10-kubeadm.conf"},
		},
		{
			// More than one component
			config: map[string]interface{}{
				"components": []string{"kubelet", "thing"},
				"kubelet":    map[string]interface{}{"svc": []string{"kubelet", "10-kubeadm.conf"}},
				"thing":      map[string]interface{}{"svc": []string{"/my/file/thing"}},
			},
			statResults: []error{os.ErrNotExist, nil, nil},
			exp:         map[string]string{"kubelet": "10-kubeadm.conf", "thing": "/my/file/thing"},
		},
		{
			// Default thing to specified default service
			config: map[string]interface{}{
				"components": []string{"kubelet", "thing"},
				"kubelet":    map[string]interface{}{"svc": []string{"kubelet", "10-kubeadm.conf"}},
				"thing":      map[string]interface{}{"svc": []string{"/my/file/thing"}, "defaultsvc": "another/thing"},
			},
			statResults: []error{os.ErrNotExist, nil, os.ErrNotExist},
			exp:         map[string]string{"kubelet": "10-kubeadm.conf", "thing": "another/thing"},
		},
		{
			// Default thing to component name
			config: map[string]interface{}{
				"components": []string{"kubelet", "thing"},
				"kubelet":    map[string]interface{}{"svc": []string{"kubelet", "10-kubeadm.conf"}},
				"thing":      map[string]interface{}{"svc": []string{"/my/file/thing"}},
			},
			statResults: []error{os.ErrNotExist, nil, os.ErrNotExist},
			exp:         map[string]string{"kubelet": "10-kubeadm.conf", "thing": "thing"},
		},
	}

	v := viper.New()
	statFunc = fakestat

	for id, c := range cases {
		t.Run(strconv.Itoa(id), func(t *testing.T) {
			for k, val := range c.config {
				v.Set(k, val)
			}
			e = c.statResults
			eIndex = 0

			m := getFiles(v, "service")
			if !reflect.DeepEqual(m, c.exp) {
				t.Fatalf("Got %v\nExpected %v", m, c.exp)
			}
		})
	}
}

func TestMakeSubsitutions(t *testing.T) {
	cases := []struct {
		input string
		subst map[string]string
		exp   string
	}{
		{input: "Replace $thisbin", subst: map[string]string{"this": "that"}, exp: "Replace that"},
		{input: "Replace $thisbin", subst: map[string]string{"this": "that", "here": "there"}, exp: "Replace that"},
		{input: "Replace $thisbin and $herebin", subst: map[string]string{"this": "that", "here": "there"}, exp: "Replace that and there"},
	}
	for _, c := range cases {
		t.Run(c.input, func(t *testing.T) {
			s := makeSubstitutions(c.input, "bin", c.subst)
			if s != c.exp {
				t.Fatalf("Got %s expected %s", s, c.exp)
			}
		})
	}
}

func TestGetConfigFilePath(t *testing.T) {
	var err error
	cfgDir, err = ioutil.TempDir("", "kube-bench-test")
	if err != nil {
		t.Fatalf("Failed to create temp directory")
	}
	defer os.RemoveAll(cfgDir)
	d := filepath.Join(cfgDir, "1.8")
	err = os.Mkdir(d, 0666)
	if err != nil {
		t.Fatalf("Failed to create temp file")
	}
	ioutil.WriteFile(filepath.Join(d, "master.yaml"), []byte("hello world"), 0666)

	cases := []struct {
		specifiedVersion string
		runningVersion   string
		succeed          bool
		exp              string
	}{
		{runningVersion: "1.8", succeed: true, exp: d},
		{runningVersion: "1.9", succeed: true, exp: d},
		{runningVersion: "1.10", succeed: true, exp: d},
		{runningVersion: "1.1", succeed: false},
		{specifiedVersion: "1.8", succeed: true, exp: d},
		{specifiedVersion: "1.9", succeed: false},
		{specifiedVersion: "1.10", succeed: false},
	}

	for _, c := range cases {
		t.Run(c.specifiedVersion+"-"+c.runningVersion, func(t *testing.T) {
			path, err := getConfigFilePath(c.specifiedVersion, c.runningVersion, "/master.yaml")
			if err != nil && c.succeed {
				t.Fatalf("Error %v", err)
			}
			if path != c.exp {
				t.Fatalf("Got %s expected %s", path, c.exp)
			}
		})
	}
}
