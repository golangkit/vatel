package vatel

// endpointController is a controller what generates
// endpoint's documentation as HTML page.

// Handle implements interface Handler.
func endpointDocumentation(e *Endpoint) func(ctx Context) error {
	return func(Context) error {

		// res := "<html><body>"
		// for i := range r {
		// 	res += fmt.Sprintf("%s %s<br>", r[i].Method, r[i].Path)
		// }

		// res += "</body></html>"
		// ctx.Response.SetStatusCode(fasthttp.StatusOK)
		// ctx.SetContentType("text/html; charset=utf-8")
		// if _, err := ctx.WriteString(res); err != nil {
		// 	return err
		// }

		return nil
	}
}
