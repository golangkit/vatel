package vatel

import (
	"context"
	"io"
	"mime/multipart"

	"github.com/valyala/fasthttp"
)

type Context interface {
	BodyWriter() io.Writer
	SetContentType([]byte) *VatelContext
	Log(key string, val interface{}) *VatelContext
	LogValues() map[string]interface{}
	SetStatusCode(code int) *VatelContext
	FormFile(key string) (*multipart.FileHeader, error)
	SaveMultipartFile(fh *multipart.FileHeader, path string) error
	Header(name string) []byte
	TokenPayload() TokenPayloader
	SetTokenPayload(tp TokenPayloader)
}

type VatelContext struct {
	context.Context
	cancel context.CancelFunc
	fh     *fasthttp.RequestCtx
	kv     map[string]interface{}
	tp     TokenPayloader
}

func NewContext(ctx *fasthttp.RequestCtx) Context {
	c := VatelContext{
		fh: ctx,
	}
	return &c
}

func (ctx *VatelContext) SetTokenPayload(tp TokenPayloader) {
	ctx.tp = tp
}

func (ctx *VatelContext) TokenPayload() TokenPayloader {
	return ctx.tp
}

func (ctx *VatelContext) FormFile(key string) (*multipart.FileHeader, error) {
	return ctx.fh.FormFile(key)
}
func (ctx *VatelContext) Header(name string) []byte {
	return ctx.fh.Request.Header.Peek(name)
}

func (ctx *VatelContext) SaveMultipartFile(fh *multipart.FileHeader, path string) error {
	return fasthttp.SaveMultipartFile(fh, path)
}

func (ctx *VatelContext) SetContentType(contentType []byte) *VatelContext {
	ctx.fh.SetContentTypeBytes(contentType)
	return ctx
}

func (ctx *VatelContext) Log(key string, val interface{}) *VatelContext {
	if ctx.kv == nil {
		ctx.kv = make(map[string]interface{}, 1)
	}
	ctx.kv[key] = val
	return ctx
}

func (ctx *VatelContext) LogValues() map[string]interface{} {
	return ctx.kv
}

func (ctx *VatelContext) BodyWriter() io.Writer {
	return ctx.fh.Response.BodyWriter()
}

func (ctx *VatelContext) SetStatusCode(code int) *VatelContext {
	ctx.fh.SetStatusCode(code)
	return ctx
}
