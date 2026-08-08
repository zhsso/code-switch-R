package services

import (
	"strings"
	"testing"
)

func TestSaveProvidersValidatesIDsAndNames(t *testing.T) {
	setupRenameTestEnv(t)
	service := NewProviderService()

	tests := []struct {
		name      string
		providers []Provider
		want      string
	}{
		{
			name:      "empty ID",
			providers: []Provider{{Name: "A", APIURL: "https://a.example"}},
			want:      "ID 不能为空",
		},
		{
			name: "duplicate ID",
			providers: []Provider{
				{ID: "same", Name: "A", APIURL: "https://a.example"},
				{ID: "same", Name: "B", APIURL: "https://b.example"},
			},
			want: "ID same 重复",
		},
		{
			name:      "empty name",
			providers: []Provider{{ID: "1", Name: " ", APIURL: "https://a.example"}},
			want:      "名称不能为空",
		},
		{
			name:      "surrounding whitespace",
			providers: []Provider{{ID: "1", Name: " A", APIURL: "https://a.example"}},
			want:      "首尾空格",
		},
		{
			name: "case insensitive duplicate name",
			providers: []Provider{
				{ID: "1", Name: "Provider", APIURL: "https://a.example"},
				{ID: "2", Name: "provider", APIURL: "https://b.example"},
			},
			want: "名称不区分大小写",
		},
	}

	for _, test := range tests {
		err := service.SaveProviders(CodexPlatform, test.providers)
		if err == nil || !strings.Contains(err.Error(), test.want) {
			t.Errorf("%s: error = %v, want substring %q", test.name, err, test.want)
		}
	}
}
