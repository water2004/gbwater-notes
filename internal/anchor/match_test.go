package anchor

import "testing"

func TestMatchEditedQuote(t *testing.T) {
	const quote = "甲乙丙丁戊己庚辛壬癸子丑寅卯辰巳"
	for _, edited := range []string{
		quote,
		"甲乙丙丁戊增加己庚辛壬癸子丑寅卯辰巳",
		"甲乙丙丁戊己辛壬癸子丑寅卯辰巳",
		"甲乙丙丁戊己庚换词癸子丑寅卯辰巳",
		"甲乙丙丁戊己新庚换壬癸子丑卯辰巳",
	} {
		text := []rune("有其他内容。原文开始" + edited + "原文结束。")
		match, ok := Match(text, NewQuery(quote, "原文开始", "原文结束"))
		if !ok || string(text[match.Start:match.End]) != edited {
			t.Fatalf("match %q: %+v, %t", edited, match, ok)
		}
	}
}

func TestMatchConfidence(t *testing.T) {
	for _, test := range []struct {
		name, body string
		query      Query
		start      int
	}{
		{"tie", "两个矩阵。两个矩阵。", NewQuery("矩阵", "两个", "。"), -1},
		{"context", "甲处矩阵定义。乙处矩阵说明。", NewQuery("矩阵", "乙处", "说明"), 9},
		{"length", "甲乙插入了太多的内容丙丁", NewQuery("甲乙丙丁", "", ""), -1},
		{"similarity", "甲乙丙丁戊一二三", NewQuery("甲乙丙丁戊己庚辛", "", ""), -1},
		{"missing", "同一篇其他完全无关的内容", NewQuery("被移除的引用", "", ""), -1},
	} {
		t.Run(test.name, func(t *testing.T) {
			match, ok := Match([]rune(test.body), test.query)
			if ok != (test.start >= 0) || (ok && match.Start != test.start) {
				t.Fatalf("location: %+v, %t", match, ok)
			}
		})
	}
}

func TestRenderedTextProjection(t *testing.T) {
	text, err := RenderedText(`<p>🧠前置 矩
		阵<img src="formula.svg" alt="图片说明"> &amp; <svg><text>图中文字</text></svg><span hidden>隐藏</span></p><script>script</script>`)
	if err != nil || text != "🧠前置矩阵&" {
		t.Fatalf("text: %q, %v", text, err)
	}
	match, ok := Match([]rune(text), NewQuery("矩 阵", "", ""))
	if !ok || match != (Range{Start: 3, End: 5}) {
		t.Fatalf("location: %+v, %t", match, ok)
	}
}
