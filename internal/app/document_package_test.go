package app

import (
	"archive/zip"
	"bytes"
	"reflect"
	"testing"
)

func TestPackageAssetLinksKeepExternalURLsAndMoveLocalImages(t *testing.T) {
	source := "![井](/school/notes/content-assets/math/%E4%BA%95.svg)\n" +
		`<img src="/mypdf/content-assets/math/a.png"> ![外部](https://example.test/content-assets/math/b.png)`
	got, ids := rewritePackageAssetLinks(source, "/school/notes/", map[string]string{"math/井.svg": "math/井-新.svg"})
	want := "![井](/content-assets/math/%E4%BA%95-%E6%96%B0.svg)\n" +
		`<img src="/content-assets/math/a.png"> ![外部](https://example.test/content-assets/math/b.png)`
	if got != want || !reflect.DeepEqual(ids, []string{"math/井.svg", "math/a.png"}) {
		t.Fatalf("rewritten links: %s, %v", got, ids)
	}
}

func TestPackageRejectsUnsafeZIPPathsAndNonStaticSVG(t *testing.T) {
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	if _, err := writer.Create("../不属于文档包.md"); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := decodeDocumentPackage(buffer.Bytes()); err == nil {
		t.Fatal("accepted a path outside the package")
	}
	svg := []byte(`<svg xmlns="http://www.w3.org/2000/svg"><g><rect fill="rgb(0%, 0%, 0%)" width="4" height="4"/><use href="#rect"/></g></svg>`)
	external := []byte(`<svg xmlns="http://www.w3.org/2000/svg"><use href="https://example.test/drawing.svg#rect"/></svg>`)
	if !validPackageSVG(svg) || validPackageSVG(external) {
		t.Fatal("unexpected SVG validation outcome")
	}
}
