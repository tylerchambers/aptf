package main

import (
	"reflect"
	"testing"

	"github.com/google/uuid"
)

func TestSourceFromString(t *testing.T) {
	type args struct {
		s            string
		uuidProvider func() uuid.UUID
	}
	tests := []struct {
		name    string
		args    args
		want    *AptSource
		wantErr bool
	}{
		{
			name: "valid source string",
			args: args{
				s: "deb http://archive.ubuntu.com/ubuntu trusty main restricted",
				uuidProvider: func() uuid.UUID {
					return uuid.MustParse("00000000-0000-0000-0000-000000000000")
				},
			},
			want: &AptSource{
				ID:         uuid.MustParse("00000000-0000-0000-0000-000000000000"),
				URI:        "http://archive.ubuntu.com/ubuntu",
				Suite:      "trusty",
				Components: []string{"main", "restricted"},
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := SourceFromString(tt.args.s, tt.args.uuidProvider)
			if (err != nil) != tt.wantErr {
				t.Errorf("SourceFromString() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("SourceFromString() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAptSourceRegistry_AddSource(t *testing.T) {
	type fields struct {
		Sources  []*AptSource
		RepoURIs []string
	}
	type args struct {
		s *AptSource
	}
	tests := []struct {
		name   string
		fields fields
		args   args
	}{
		{
			name: "add source",
			fields: fields{
				Sources:  []*AptSource{},
				RepoURIs: []string{},
			},
			args: args{
				s: &AptSource{
					URI:        "http://archive.ubuntu.com/ubuntu",
					Suite:      "trusty",
					Components: []string{"main", "restricted"},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := &AptSourceRegistry{
				Sources:  tt.fields.Sources,
				RepoURIs: tt.fields.RepoURIs,
			}
			a.AddSource(tt.args.s)
		})
	}
}

func TestAptSourceRegistry_GenerateRepoURIs(t *testing.T) {
	type fields struct {
		Sources  []*AptSource
		RepoURIs []string
	}
	tests := []struct {
		name   string
		fields fields
		want   []string
	}{
		{
			name: "generate repo uris",
			fields: fields{
				Sources: []*AptSource{
					{
						URI:        "http://archive.ubuntu.com/ubuntu",
						Suite:      "trusty",
						Components: []string{"main", "restricted"},
					},
				},
				RepoURIs: []string{},
			},
			want: []string{
				"http://archive.ubuntu.com/ubuntu/dists/trusty/main",
				"http://archive.ubuntu.com/ubuntu/dists/trusty/restricted",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := &AptSourceRegistry{
				Sources:  tt.fields.Sources,
				RepoURIs: tt.fields.RepoURIs,
			}
			a.GenerateRepoURIs()
			if !reflect.DeepEqual(a.RepoURIs, tt.want) {
				t.Errorf("AptSourceRegistry.GenerateRepoURIs() = %v, want %v", a.RepoURIs, tt.want)
			}
		})
	}
}
