package vatel

import (
	"fmt"
	"io"
)

// tocController is a controller what generates table of content
// of endpoint documentation as HTML page.
type tocController struct {
	s *Vatel
}

// Handle implements interface Handler.
func (toc *tocController) Handle(ctx Context) error {
	r := make([]Endpoint, len(toc.s.ep))
	copy(r, toc.s.ep)

	res := "<html><body>"
	for i := range r {
		res += fmt.Sprintf("%s %s<br>", r[i].Method, r[i].Path)
	}

	res += "</body></html>"
	ctx.SetStatusCode(200).SetContentType([]byte("text/html; charset=utf-8"))

	if _, err := io.WriteString(ctx.BodyWriter(), res); err != nil {
		return err
	}

	return nil
}
