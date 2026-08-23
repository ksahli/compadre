package web

import (
	"html"
	"strings"
)

// dropped are the elements whose contents are not text at all. What is inside
// them would otherwise arrive as a wall of code the model has to read before
// it can find the page.
//
// head is not among them, though it is mostly machinery: what it holds that is
// not machinery is the title, which is often the plainest statement of what a
// page is. The parts of a head worth dropping — script, style — are dropped by
// name anyway, and the rest of it carries its text in attributes this
// reduction never reads.
var dropped = map[string]bool{
	"script":   true,
	"style":    true,
	"noscript": true,
	"template": true,
	"svg":      true,
}

// blocks are the elements that stand on their own line. The reduction breaks
// on both their open and their close and lets the tidying collapse whatever
// doubling that causes, which is cheaper than reasoning about which of the two
// a given page actually spelled.
var blocks = map[string]bool{
	"p": true, "div": true, "li": true, "tr": true, "br": true, "hr": true,
	"section": true, "article": true, "header": true, "footer": true,
	"nav": true, "aside": true, "main": true, "form": true,
	"ul": true, "ol": true, "dl": true, "dt": true, "dd": true,
	"table": true, "thead": true, "tbody": true, "figure": true,
	"blockquote": true, "pre": true, "title": true,
	"h1": true, "h2": true, "h3": true, "h4": true, "h5": true, "h6": true,
}

// flatten reduces HTML to the text in it.
//
// It is not a parser and does not pretend to be one: the standard library has
// no HTML tokenizer, and taking a dependency to get one would buy accuracy
// this does not need. What it is is a way to spend less of the model's
// context — markup is most of a page's bytes and almost none of its meaning,
// and a model handed a megabyte of div soup is not reading any of it.
//
// So the failure mode is stated rather than hidden: a malformed page leaks
// some markup through as text. It never panics and never loops, which is the
// whole of what it promises.
func flatten(source string) string {
	var out strings.Builder
	out.Grow(len(source) / 2)

	// skip is the element whose contents are being dropped, empty when
	// nothing is. Nesting inside a dropped element does not matter: what
	// ends the skip is that element's own close.
	skip := ""

	for i := 0; i < len(source); {
		if source[i] != '<' {
			if skip == "" {
				out.WriteByte(source[i])
			}
			i++
			continue
		}

		// A comment, and everything a comment can hide. An unterminated
		// one swallows the rest of the page, which is what a browser
		// does with it too.
		if strings.HasPrefix(source[i:], "<!--") {
			end := strings.Index(source[i+4:], "-->")
			if end < 0 {
				break
			}
			i += 4 + end + len("-->")
			continue
		}
		// A doctype or a processing instruction: no name, nothing to
		// say, read to the end of it.
		if strings.HasPrefix(source[i:], "<!") || strings.HasPrefix(source[i:], "<?") {
			i = past(source, i)
			continue
		}

		name, closing, closed, next := element(source, i)
		// A '<' that begins no tag is a '<' somebody meant, as in
		// 'a < b'. It is text.
		if name == "" {
			if skip == "" {
				out.WriteByte(source[i])
			}
			i++
			continue
		}
		i = next

		if skip != "" {
			if closing && name == skip {
				skip = ""
			}
			continue
		}
		// A dropped element that closed itself has no contents to
		// drop. Entering the skip on it would wait for a close that is
		// never coming and swallow the rest of the page — the failure
		// this reduction promises not to have, since '<svg/>' is a
		// page spelled correctly and not a malformed one.
		if !closing && !closed && dropped[name] {
			skip = name
			continue
		}

		switch {
		case blocks[name]:
			out.WriteByte('\n')
		case name == "td" || name == "th":
			out.WriteByte(' ')
		}
	}

	// Unescaping last is what makes it safe: by now there is no markup
	// left to confuse, so an '&lt;script&gt;' the page meant literally
	// becomes the text it was rather than a tag this pass would have acted
	// on.
	return tidy(html.UnescapeString(out.String()))
}

// element reads one tag, which source[i] is the '<' of. Quotes are tracked so
// an attribute holding a '>' does not end the tag early — the case that turns
// a naive strip into markup on the model's screen. An unterminated tag runs to
// the end of the source rather than being handed back as text.
//
// closed says the tag closed itself, which is read off the '/' immediately
// before the '>'. Quoting is what keeps that honest: a href ending in a slash
// puts the quote there instead, so '<a href="/docs/">' is not mistaken for a
// tag with no contents.
func element(source string, i int) (name string, closing, closed bool, next int) {
	j := i + 1
	if j < len(source) && source[j] == '/' {
		closing = true
		j++
	}

	start := j
	for j < len(source) && named(source[j]) {
		j++
	}
	if j == start {
		return "", false, false, i
	}

	next = past(source, j)
	closed = next-2 >= start && source[next-1] == '>' && source[next-2] == '/'

	return strings.ToLower(source[start:j]), closing, closed, next
}

// past walks to just beyond the '>' that ends the tag begun at or before i,
// minding quoted attribute values on the way.
func past(source string, i int) int {
	quote := byte(0)
	for ; i < len(source); i++ {
		switch {
		case quote != 0:
			if source[i] == quote {
				quote = 0
			}
		case source[i] == '"' || source[i] == '\'':
			quote = source[i]
		case source[i] == '>':
			return i + 1
		}
	}
	return len(source)
}

// named says whether a byte can be part of an element name. Anything else is
// where the name ends.
func named(c byte) bool {
	return c >= 'a' && c <= 'z' ||
		c >= 'A' && c <= 'Z' ||
		c >= '0' && c <= '9'
}

// tidy turns what the reduction left into something worth reading: runs of
// whitespace collapsed, lines trimmed, and no more than one blank line
// anywhere. Whitespace in HTML is mostly the author's indentation rather than
// anything they meant, and passing it on would spend the model's context on
// the shape of somebody's template.
func tidy(text string) string {
	lines := strings.Split(text, "\n")
	out := make([]string, 0, len(lines))

	for _, line := range lines {
		line = strings.TrimSpace(collapse(line))
		if line == "" {
			// A blank line before anything is nothing; a run of
			// them in the middle is one.
			if len(out) == 0 || out[len(out)-1] == "" {
				continue
			}
		}
		out = append(out, line)
	}

	for len(out) > 0 && out[len(out)-1] == "" {
		out = out[:len(out)-1]
	}

	return strings.Join(out, "\n")
}

// collapse reduces every run of blank space within one line to a single space.
func collapse(line string) string {
	var out strings.Builder
	out.Grow(len(line))

	space := false
	for _, r := range line {
		if r == ' ' || r == '\t' || r == '\r' || r == '\v' || r == '\f' || r == '\u00a0' {
			space = true
			continue
		}
		if space && out.Len() > 0 {
			out.WriteByte(' ')
		}
		space = false
		out.WriteRune(r)
	}

	return out.String()
}
