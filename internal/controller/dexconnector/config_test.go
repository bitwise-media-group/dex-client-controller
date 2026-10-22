// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package dexconnector

import "testing"

func TestBuildConfig(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		values  []FieldValue
		want    string
		wantErr bool
	}{
		{
			name: "empty config",
			want: `{}`,
		},
		{
			name:   "top-level field, keys sorted",
			raw:    `{"redirectURI": "https://dex/callback", "clientID": "abc", "issuer": "https://idp"}`,
			values: []FieldValue{{Path: "clientSecret", Value: "s3cret"}},
			want:   `{"clientID":"abc","clientSecret":"s3cret","issuer":"https://idp","redirectURI":"https://dex/callback"}`,
		},
		{
			name:   "overwrites an inline value",
			raw:    `{"bindPW": "placeholder"}`,
			values: []FieldValue{{Path: "bindPW", Value: "real"}},
			want:   `{"bindPW":"real"}`,
		},
		{
			name:   "nested path into an existing object",
			raw:    `{"bind": {"dn": "cn=admin"}}`,
			values: []FieldValue{{Path: "bind.pw", Value: "x"}},
			want:   `{"bind":{"dn":"cn=admin","pw":"x"}}`,
		},
		{
			name:   "nested path creates intermediate objects",
			values: []FieldValue{{Path: "a.b.c", Value: "x"}, {Path: "a.d", Value: "y"}},
			want:   `{"a":{"b":{"c":"x"},"d":"y"}}`,
		},
		{
			name:   "null intermediate is replaced",
			raw:    `{"a": null}`,
			values: []FieldValue{{Path: "a.b", Value: "x"}},
			want:   `{"a":{"b":"x"}}`,
		},
		{
			name: "large integers and HTML characters survive",
			raw:  `{"orgID": 12345678901234567890, "filter": "(&(a=b)<c>)"}`,
			want: `{"filter":"(&(a=b)<c>)","orgID":12345678901234567890}`,
		},
		{
			name:    "path through a scalar",
			raw:     `{"bind": "not-an-object"}`,
			values:  []FieldValue{{Path: "bind.pw", Value: "x"}},
			wantErr: true,
		},
		{
			name:    "path through an array",
			raw:     `{"orgs": [{"name": "a"}]}`,
			values:  []FieldValue{{Path: "orgs.0", Value: "x"}},
			wantErr: true,
		},
		{name: "not an object", raw: `["a"]`, wantErr: true},
		{name: "invalid JSON", raw: `{"a":`, wantErr: true},
		{name: "trailing data", raw: `{} {}`, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := BuildConfig([]byte(tt.raw), tt.values)
			if (err != nil) != tt.wantErr {
				t.Fatalf("BuildConfig error = %v, wantErr %v", err, tt.wantErr)
			}
			if err == nil && string(got) != tt.want {
				t.Errorf("BuildConfig = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestNormalize(t *testing.T) {
	a, err := Normalize([]byte("{\n  \"b\": [1, 2.50],\n  \"a\": {\"y\": true, \"x\": null}\n}"))
	if err != nil {
		t.Fatal(err)
	}
	b, err := Normalize([]byte(`{"a":{"x":null,"y":true},"b":[1,2.50]}`))
	if err != nil {
		t.Fatal(err)
	}
	if string(a) != string(b) {
		t.Errorf("Normalize differs for equivalent configs: %s vs %s", a, b)
	}
	if _, err := Normalize([]byte("nope")); err == nil {
		t.Error("Normalize accepted invalid JSON")
	}
}
