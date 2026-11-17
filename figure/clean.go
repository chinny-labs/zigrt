// SPDX-License-Identifier: Apache-2.0

package figure

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"html"
	"io"
	"strings"
)

// A static figure from a content pack is untrusted input (plan 10.2). Clean
// keeps only drawing elements and geometry and paint attributes, and fails
// on anything else: scripts, styles, links, embedded documents, event
// handlers, or a url() reference.

var allowedElements = map[string]bool{
	"svg": true, "g": true, "path": true, "rect": true, "circle": true, "ellipse": true,
	"line": true, "polyline": true, "polygon": true, "text": true, "tspan": true,
	"title": true, "desc": true,
}

var allowedAttrs = map[string]bool{
	"viewBox": true, "width": true, "height": true, "x": true, "y": true, "x1": true, "y1": true,
	"x2": true, "y2": true, "cx": true, "cy": true, "r": true, "rx": true, "ry": true, "d": true,
	"points": true, "transform": true, "fill": true, "stroke": true, "stroke-width": true,
	"stroke-linecap": true, "stroke-linejoin": true, "stroke-dasharray": true, "opacity": true,
	"fill-opacity": true, "stroke-opacity": true, "fill-rule": true, "text-anchor": true,
	"font-size": true, "font-weight": true, "dominant-baseline": true, "class": true,
	"role": true, "aria-label": true, "dx": true, "dy": true,
}

// MaxStaticSize bounds a static figure file.
const MaxStaticSize = 64 << 10

// Clean returns a static SVG rewritten from the allowed parts, or an error
// naming the first thing that is not allowed.
func Clean(src []byte) (string, error) {
	if len(src) > MaxStaticSize {
		return "", fmt.Errorf("figure: %d bytes is more than %d", len(src), MaxStaticSize)
	}
	dec := xml.NewDecoder(bytes.NewReader(src))
	dec.Strict = true
	var out strings.Builder
	depth, roots := 0, 0
	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return "", fmt.Errorf("figure: %w", err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			name := t.Name.Local
			if !allowedElements[name] || (t.Name.Space != "" && t.Name.Space != "http://www.w3.org/2000/svg") {
				return "", fmt.Errorf("figure: <%s> is not allowed", name)
			}
			if depth == 0 {
				if name != "svg" {
					return "", errors.New("figure: the root must be <svg>")
				}
				roots++
			}
			out.WriteString("<" + name)
			if depth == 0 {
				out.WriteString(` xmlns="http://www.w3.org/2000/svg" class="fig fig-static" role="img"`)
			}
			for _, a := range t.Attr {
				if a.Name.Space == "xmlns" || (a.Name.Space == "" && a.Name.Local == "xmlns") {
					continue
				}
				if a.Name.Space != "" || !allowedAttrs[a.Name.Local] {
					return "", fmt.Errorf("figure: the %s attribute is not allowed", a.Name.Local)
				}
				if depth == 0 && (a.Name.Local == "class" || a.Name.Local == "role") {
					continue
				}
				v := strings.ToLower(a.Value)
				if strings.Contains(v, "url(") || strings.Contains(v, "javascript:") || strings.Contains(v, "expression(") {
					return "", fmt.Errorf("figure: %s=%q refers outside the figure", a.Name.Local, a.Value)
				}
				out.WriteString(" " + a.Name.Local + `="` + html.EscapeString(a.Value) + `"`)
			}
			out.WriteString(">")
			depth++
		case xml.EndElement:
			out.WriteString("</" + t.Name.Local + ">")
			depth--
		case xml.CharData:
			if depth > 0 {
				out.WriteString(html.EscapeString(string(t)))
			}
		case xml.Comment, xml.ProcInst:
			// dropped
		case xml.Directive:
			return "", errors.New("figure: a DOCTYPE or other directive is not allowed")
		}
	}
	if roots != 1 {
		return "", errors.New("figure: expected one <svg> element")
	}
	return out.String(), nil
}
