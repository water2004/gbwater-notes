package app

import "testing"

func TestParseMetadataAcceptsCRLF(t *testing.T) {
	content := "---\r\ntitle: \"极限\"\r\ncategory: \"math\"\r\ncategoryTitle: \"数学\"\r\ncourse: \"数学分析B1\"\r\norder: 1\r\ndraft: false\r\n---\r\n\r\n正文\r\n"
	note, err := parseMetadata("math/数学分析B1/极限", content, nil)
	if err != nil {
		t.Fatal(err)
	}
	if note.Title != "极限" || note.Course != "数学分析B1" {
		t.Fatalf("unexpected note: %+v", note)
	}
}
