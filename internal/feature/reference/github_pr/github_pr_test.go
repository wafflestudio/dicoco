package githubpr

import (
	"reflect"
	"testing"
)

func TestExtractPRURLs(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    []string
	}{
		{
			name:    "single pr url",
			content: "https://github.com/wafflestudio/snutt/pull/545",
			want:    []string{"github.com/wafflestudio/snutt/pull/545"},
		},
		{
			name:    "pr url without https scheme",
			content: "github.com/wafflestudio/snutt/pull/545 봐주세요",
			want:    []string{"github.com/wafflestudio/snutt/pull/545"},
		},
		{
			name:    "pr url in the middle of conversation",
			content: "이번에 올린 https://github.com/wafflestudio/snutt/pull/545 리뷰 부탁드려요!",
			want:    []string{"github.com/wafflestudio/snutt/pull/545"},
		},
		{
			name:    "pr url with subpath or fragment",
			content: "코멘트 확인해주세요 <https://github.com/wafflestudio/dicoco/pull/12/files#diff-123>",
			want:    []string{"github.com/wafflestudio/dicoco/pull/12"},
		},
		{
			name:    "multiple pr urls with duplicate",
			content: "https://github.com/wafflestudio/snutt/pull/545 그리고 https://github.com/wafflestudio/snutt/pull/545/commits 또 https://github.com/wafflestudio/dicoco/pull/7",
			want: []string{
				"github.com/wafflestudio/snutt/pull/545",
				"github.com/wafflestudio/dicoco/pull/7",
			},
		},
		{
			name:    "non-wafflestudio pr url ignored",
			content: "https://github.com/golang/go/pull/12345",
			want:    nil,
		},
		{
			name:    "wafflestudio issue url ignored",
			content: "https://github.com/wafflestudio/snutt/issues/545",
			want:    nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractPRURLs(tt.content)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("extractPRURLs() = %v, want %v", got, tt.want)
			}
		})
	}
}
