package web

import (
	"strings"
	"testing"
)

// TestFlatten walks the reduction over the shapes a real page comes in. What
// is pinned throughout is the promise the package makes about it: not that it
// parses HTML, which it does not, but that markup does not reach the model as
// text and that a page is never silently lost.
func TestFlatten(t *testing.T) {
	cases := []struct {
		name   string
		source string
		want   string
	}{
		{
			"tags come out and their text stays",
			`<p>hello <b>there</b></p>`,
			"hello there",
		},
		{
			// A block breaks on both its open and its close, so
			// two of them meeting leave a blank line between
			// them. That is the paragraph break the model reads.
			"a block element stands on its own line",
			`<p>one</p><p>two</p>`,
			"one\n\ntwo",
		},
		{
			"headings are blocks too",
			`<h1>Title</h1><p>body</p>`,
			"Title\n\nbody",
		},
		{
			"a break breaks",
			`<p>a<br/>b</p>`,
			"a\nb",
		},
		{
			"cells are separated but not broken",
			`<table><tr><td>a</td><td>b</td></tr></table>`,
			"a b",
		},
		{
			"the title survives, since a head is not all machinery",
			`<html><head><meta charset="utf-8"><title>What This Is</title></head>` +
				`<body><p>body</p></body></html>`,
			"What This Is\n\nbody",
		},
		{
			"script contents are dropped whole",
			`<p>before</p><script>if (a < b) { document.write("<p>hi</p>") }</script><p>after</p>`,
			"before\n\nafter",
		},
		{
			"style contents are dropped whole",
			`<p>before</p><style>p { content: "<b>"; }</style><p>after</p>`,
			"before\n\nafter",
		},
		{
			"noscript, template and svg go the same way",
			`<p>a</p><noscript>n</noscript><template>t</template><svg><path d="M0 0"/></svg><p>b</p>`,
			"a\n\nb",
		},
		{
			// The regression the whole self-closing check is for. A
			// page spelled correctly must not lose everything after
			// an inline icon.
			"a self-closing dropped element does not swallow the page",
			`<p>before</p><svg viewBox="0 0 16 16"/><p>after</p>`,
			"before\n\nafter",
		},
		{
			"nesting inside a dropped element does not end the skip early",
			`<p>a</p><template><div>x</div><template>y</template></template><p>b</p>`,
			"a\n\nb",
		},
		{
			"a comment is not text",
			`<p>a</p><!-- <p>not this</p> --><p>b</p>`,
			"a\n\nb",
		},
		{
			// The same thing a browser does with it, and the reason
			// the loop breaks rather than treating the '<' as text.
			"an unterminated comment swallows what follows",
			`<p>a</p><!-- and then nothing closed it <p>b</p>`,
			"a",
		},
		{
			"a doctype says nothing",
			`<!DOCTYPE html><p>a</p>`,
			"a",
		},
		{
			"a processing instruction says nothing either",
			`<?xml version="1.0"?><p>a</p>`,
			"a",
		},
		{
			"a less-than somebody meant is text",
			`<p>a < b and c > d</p>`,
			"a < b and c > d",
		},
		{
			// The case that turns a naive strip into markup on the
			// model's screen: the '>' is inside a quoted value and
			// does not end the tag.
			"an attribute holding a bracket does not end the tag early",
			`<a title="a > b" href="/x">link</a>`,
			"link",
		},
		{
			"an attribute holding a bracket in single quotes either",
			`<a title='a > b'>link</a>`,
			"link",
		},
		{
			"an unterminated tag runs to the end rather than leaking",
			`<p>a</p><div class="never closed`,
			"a",
		},
		{
			// Unescaping last is what makes this safe: by the time
			// the entities are read there is no pass left to mistake
			// them for markup.
			"entities are text, not a second pass of markup",
			`<p>&lt;script&gt;alert(1)&lt;/script&gt;</p>`,
			"<script>alert(1)</script>",
		},
		{
			"the ordinary entities come back as characters",
			`<p>Ben &amp; Jerry&#39;s &mdash; &quot;good&quot;</p>`,
			`Ben & Jerry's — "good"`,
		},
		{
			"a template's indentation is not the author's meaning",
			"<div>\n\t\t<p>   a     b   </p>\n\n\n\t<p>c</p>\n</div>",
			"a b\n\nc",
		},
		{
			"a non-breaking space is still a space",
			"<p>a  b</p>",
			"a b",
		},
		{
			// Empty blocks are the shape of most templates, and
			// the tidying is what keeps them from arriving as a
			// column of nothing.
			"blank lines never run more than one deep",
			`<div></div><div></div><p>a</p><div></div><div></div><p>b</p>`,
			"a\n\nb",
		},
		{
			"nothing in is nothing out",
			``,
			"",
		},
		{
			"markup with no text in it is no text",
			`<div><span></span></div>`,
			"",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := flatten(c.source); got != c.want {
				t.Errorf("flatten(%q) = %q, want %q", c.source, got, c.want)
			}
		})
	}
}

// TestFlattenNeverLeavesATag is the promise stated the other way round. The
// pages here are malformed in the ways pages are, and the reduction is allowed
// to make a worse answer of them — it is not allowed to hand back a tag.
func TestFlattenNeverLeavesATag(t *testing.T) {
	pages := []string{
		`<p>a<p>b<p>c`,
		`<div><div><div>deep`,
		`</p></div></span>`,
		`<p class=unquoted>a</p>`,
		`<P>SHOUTING</P>`,
		`<p >spaced</p >`,
		`<img src="x.png" alt="a > b"><p>a</p>`,
		`<p>a</p><script src="x.js"></script><p>b</p>`,
	}

	for _, page := range pages {
		t.Run(page, func(t *testing.T) {
			got := flatten(page)
			if strings.ContainsAny(got, "<>") {
				t.Errorf("flatten(%q) = %q, want no markup left in it", page, got)
			}
		})
	}
}

// TestElement pins the tag reader, and mostly pins the one thing it is easy to
// get wrong: telling a tag that closed itself from a tag whose last attribute
// happens to end in a slash.
func TestElement(t *testing.T) {
	cases := []struct {
		source  string
		name    string
		closing bool
		closed  bool
		next    int
	}{
		{`<p>`, "p", false, false, 3},
		{`</p>`, "p", true, false, 4},
		{`<BR>`, "br", false, false, 4},
		{`<br/>`, "br", false, true, 5},
		{`<br />`, "br", false, true, 6},
		{`<svg viewBox="0 0"/>`, "svg", false, true, 20},
		// The trap: the slash is inside the quotes, and the tag is not
		// closed at all.
		{`<a href="/docs/">`, "a", false, false, 17},
		{`<a href='/docs/'>`, "a", false, false, 17},
		// A '<' that begins no tag is a '<' somebody meant. next stays
		// where it was so the caller writes the byte and moves on by
		// one itself.
		{`< b`, "", false, false, 0},
		{`<`, "", false, false, 0},
		// An unterminated tag runs to the end rather than being handed
		// back as text.
		{`<div class="x`, "div", false, false, 13},
	}

	for _, c := range cases {
		t.Run(c.source, func(t *testing.T) {
			name, closing, closed, next := element(c.source, 0)
			if name != c.name || closing != c.closing || closed != c.closed || next != c.next {
				t.Errorf("element(%q, 0) = (%q, %v, %v, %d), want (%q, %v, %v, %d)",
					c.source, name, closing, closed, next,
					c.name, c.closing, c.closed, c.next)
			}
		})
	}
}
