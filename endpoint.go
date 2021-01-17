package vatel

import (
	"encoding/json"
	"fmt"
	"path"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/axkit/errors"
	"github.com/rs/zerolog"
	goon "github.com/shurcooL/go-goon"
	"github.com/valyala/fasthttp"
)

// Endpoint describes a REST endpoint attributes and related request Handler.
type Endpoint struct {
	// Method holds HTTP method name (e.g GET, POST, PUT, DELETE).
	Method string

	// Path holds url path with fasthttp parameters (e.g. /customers/{id}).
	Path string

	// Perms holds list of permissions. Nil if endpoint is public.
	Perms []string

	// Controller holds reference to the object impementing interface Handler.
	Controller func() Handler

	// ResponseContentType by default has "application/json; charset: utf-8;"
	ResponseContentType string
	responseContentType []byte

	// NoInputLog defines debug logging rule for request data. If true, endpoint request body
	// will not be written to the log. (i.e authentication endpoint).
	NoInputLog bool

	// NoResultLog defines debug logging rule for response data. If true, endpoint response body
	// will not be written to the log. (i.e authentication  endpoint)
	NoResultLog bool

	//RequestContentType  string // если пусто, то присваивается "application/json; encoding=utf-8"

	isPathParametrized    bool
	isURLQueryExpected    bool
	isRequestBodyExpected bool
	isResulter            bool

	LanguageLabel string // #Cannon
	auth          Authorizer
	td            TokenDecoder
	pm            PermissionManager
	rd            RequestDebugger
	perms         []uint
}

// NewEnpoint builds Endpoint.
func NewEnpoint(method, path string, perms []string, c func() Handler) *Endpoint {
	return &Endpoint{Method: method, Path: path, Perms: append([]string{}, perms...), Controller: c}
}

// Endpointer is the interface that wraps a single Endpoints method.
//
// Endpoints returns []Endpoints to be handled by API gateway.
type Endpointer interface {
	Endpoints() []Endpoint
}

// Handler is the interface what wraps Handle method.
//
// Handle invocates by API gateway mux.
type Handler interface {
	Handle(Context) error
}

// Inputer is the interface what wraps Input method.
//
// Input returns reference to the object what will be promoted
// with input data by vatel.
//
// If endpoint's handler expects input data, Input method should be
// implemented.
//
// GET, DELETE methods:  input values will be taken from URL query.
// POST, PUT, PATCH methods: input values will be taken from JSON body.
type Inputer interface {
	Input() interface{}
}

// Resulter is the interface what wraps Result method.
//
// Result returns reference to the object what will be
// send to the client when endpoint handler completes succesfully.
//
// If endpoint's controller have outgoing data, Result method should be implemented.
type Resulter interface {
	Result() interface{}
}

// Paramer is the interface what wraps a single Param method.
//
// Param returns reference to the struct what will be promoted with
// values from URL.
//
// Example: if we have /customer/{id}/bill/{billnum} then
// Param() should return reference to struct
// {
//		CustomerID int `param:"id"
//	 	BillNum string `param:"billnum"`
// }
type Paramer interface {
	Param() interface{}
}

// Error overloading for zerolog.Implementation
type Error struct {
	errors.CatchedError
}

func (ce *Error) JSON() interface{} {

	return &struct {
		Message string   `json:"message"`
		Code    string   `json:"code,omitempty"`
		Prev    []string `json:"prev,omitempty"`
	}{Message: ce.Last().Message,
		Code: ce.Last().Code,
		Prev: ce.Strs(true),
	}
}

func (ce *Error) MarshalZerologObject(e *zerolog.Event) {
	if ce == nil {
		return
	}

	e.Str("severity", ce.Last().Severity.String())
	if ce.Last().Code != "" {
		e.Str("errcode", ce.Last().Code)
	}

	if len(ce.Fields) > 0 {
		e.Fields(ce.Fields)
	}

	if ce.Last().StatusCode != 0 {
		e.Int("statusCode", ce.Last().StatusCode)
	}

	if ce.Len() > 1 {
		e.Strs("errs", ce.Strs(false))
	}

	s := ""
	for i := range ce.Frames {
		if strings.Contains(ce.Frames[i].Function, "fasthttp") {
			break
		}
		s += ce.Frames[i].Function + "() in " + fmt.Sprintf("%s:%d; ", ce.Frames[i].File, ce.Frames[i].Line)
	}
	e.Str("stack", s)

}

func writeErrorResponse(ctx Context, log *zerolog.Logger, err error) {
	if err == nil {
		return
	}

	ee, ok := err.(*errors.CatchedError)
	if ok {
		log.Error().EmbedObject(&Error{*ee}).Msg(ee.Last().Message)
		ctx.SetStatusCode(ee.Last().StatusCode)

		enc := json.NewEncoder(ctx.BodyWriter())
		if err := enc.Encode((&Error{*ee}).JSON()); err != nil {
			fmt.Println(err.Error())
		}
		return
	}

	log.Error().Msg(err.Error())

	enc := json.NewEncoder(ctx.BodyWriter())
	if err := enc.Encode((&Error{*errors.Catch(err)}).JSON()); err != nil {
		fmt.Println(err.Error())
	}
}

func (e *Endpoint) handler(l *zerolog.Logger) func(*fasthttp.RequestCtx) {

	return func(fctx *fasthttp.RequestCtx) {

		inDebug, outDebug := !e.NoInputLog, !e.NoResultLog

		logger := l.With().Uint64("reqid", fctx.ID()).Logger()
		logger.Info().
			Bool("private", len(e.Perms) > 0).
			IPAddr("from", fctx.RemoteIP()).
			Msg("new request")

		ctx := NewContext(fctx)
		if len(e.Perms) > 0 && e.auth != nil {
			token, err := e.authorize(fctx)
			if err != nil {
				writeErrorResponse(ctx, &logger, err)
				return
			}
			if e.rd != nil {
				inDebug, outDebug = e.rd.IsDebugRequired(token.ApplicationPayload())
			}

			ctx.SetTokenPayload(token.ApplicationPayload())
		}

		if fctx.QueryArgs().GetBool("description") {
			if err := e.handleDescription(ctx); err != nil {
				writeErrorResponse(ctx, &logger, err)
			}
			return
		}

		c, err := e.initController(fctx, &logger, inDebug)
		if err != nil {
			writeErrorResponse(ctx, &logger, err)
			return
		}

		if err = c.Handle(ctx); err != nil {
			writeErrorResponse(ctx, &logger, err)
			return
		}

		var elog *zerolog.Event
		if outDebug {
			elog = logger.Debug()
		} else {
			elog = logger.Info()
		}
		resp, ok := c.(Resulter)
		if ok {
			r := resp.Result()
			if outDebug {
				elog.Interface("result", r)
			}

			fctx.SetContentTypeBytes(e.responseContentType)
			if err := json.NewEncoder(ctx.BodyWriter()).Encode(resp.Result()); err != nil {
				writeErrorResponse(ctx, &logger, err)
				return
			}
		} else {
			// если тела ответа в виде JSON объекта не предполагается, то ожидается
			// что сам обработчик obj.Haldler() установит соответствующие статусы
			// и запишет в тело ответа соответствующие заголовки.
			// например обработчик отдачи файлов.
		}

		if kv := ctx.LogValues(); len(kv) > 0 {
			elog.Interface("hval", kv)
		}

		elog.Str("dur", time.Since(fctx.Time()).String()).Msg("completed")
	}
}

func (e *Endpoint) authorize(ctx *fasthttp.RequestCtx) (Tokener, error) {

	token, err := e.td.Decode(ctx.Request.Header.Peek("Authorization"))
	if err != nil {
		return nil, errors.Catch(err).Msg("unauthorized").SetStrs("perms", e.Perms...)
	}

	isAllowed, err := e.auth.IsAllowed(token.ApplicationPayload().Perms(), e.perms...)
	if err == nil {
		if isAllowed {
			return token, nil
		}
		return nil, errors.Forbidden().
			Set("user", token.ApplicationPayload().Login()).
			Set("role", token.ApplicationPayload().Role()).
			SetStrs("perms", e.Perms...)
	}

	return nil, errors.Catch(err).
		Set("user", token.ApplicationPayload().Login()).
		Set("role", token.ApplicationPayload().Role()).
		SetStrs("perms", e.Perms...).
		StatusCode(401)

}

func (e *Endpoint) initController(ctx *fasthttp.RequestCtx, plog *zerolog.Logger, debug bool) (Handler, error) {

	var elog *zerolog.Event
	if debug {
		elog = plog.Debug()
	}

	c := e.Controller()

	if e.isPathParametrized {
		p := c.(Paramer).Param()
		if err := decodeParams(ctx, p); err != nil {
			return nil, err
		}
		if debug {
			elog.Interface("param", p)
		}
	}

	if e.isURLQueryExpected {
		in := c.(Inputer).Input()
		if err := decodeURLQuery(ctx, in); err != nil {
			return nil, err
		}
		if debug {
			elog.Interface("urlquery", in)
		}
	}

	if e.isRequestBodyExpected {
		in := c.(Inputer).Input()
		if err := decodeBody(ctx, in); err != nil {
			return nil, err
		}
		if debug && !e.NoInputLog {
			elog.Interface("body", in)
		}
	}

	if debug && (e.isPathParametrized || e.isURLQueryExpected || e.isRequestBodyExpected) {
		elog.Msg("request parsed")
	}

	return c, nil
}

// Handler реализует типовой обработчик HTTP запроса, враппер вокруг fasthttp.
// func BaseHandler(r *Route, l *zerolog.Logger) func(*fasthttp.RequestCtx) {

// 	_, param := r.Controller().(Paramer)
// 	r.isPathParametrized = strings.Contains(r.Path, "/:") && param

// 	_, input := r.Controller().(Inputer)
// 	if r.Method == "GET" {
// 		r.isURLQueryExpected = input
// 	} else {
// 		r.isRequestBodyExpected = input
// 	}

// 	return func(ctx *fasthttp.RequestCtx) {
// 		t := time.Now()
// 		logger := l.With().Str("path", r.Path).Str("method", r.Method).Uint64("reqid", ctx.ID()).IPAddr("ip", ctx.RemoteIP()).Logger()

// 		if len(r.Perms) > 0 {
// 			// получаем параметры сессии (для определения прав доступных пользователю)
// 			// и проверяем наличие у пользователя перма среди требуемых r.Perm
// 		}

// 		if ctx.QueryArgs().GetBool("description") == true {
// 			logger.Info().Msg("description requested")
// 			ctx.Response.Header.SetContentType("text/html; charset=utf8")
// 			_, err := ctx.Write(Doc(r))
// 			if err != nil {
// 				logger.Error().Str("errmsg", err.Error()).Msg("description response write failed")
// 			}
// 			return
// 		}

// 		obj := r.Controller()

// 		if r.isPathParametrized {

// 			if err := assignParams(ctx, obj.(Paramer).Param()); err != nil {
// 				logger.Error().Str("errmsg", err.Error()).Msg("invalid path parameter")
// 				ctx.Response.SetStatusCode(fasthttp.StatusBadRequest)
// 				ctx.WriteString(err.Error())
// 				return
// 			}
// 		}

// 		if r.isURLQueryExpected {
// 			if err := assignURLInput(ctx, obj.(Inputer).Input()); err != nil {
// 				logger.Error().Str("errmsg", err.Error()).Msg("invalid url query parameter")
// 				ctx.Response.SetStatusCode(fasthttp.StatusBadRequest)
// 				ctx.WriteString(err.Error())
// 				return
// 			}
// 		}

// 		if r.isRequestBodyExpected {
// 			if err := assignBodyInput(ctx, obj.(Inputer).Input()); err != nil {
// 				logger.Error().Str("errmsg", err.Error()).Msg("invalid body input data")
// 				ctx.Response.SetStatusCode(fasthttp.StatusBadRequest)
// 				ctx.WriteString(err.Error())
// 				return
// 			}
// 		}

// 		if err := obj.Handle(ctx); err != nil {
// 			logger.Error().Str("errmsg", err.Error()).Msg("invalid body input data")
// 			ctx.Response.SetStatusCode(fasthttp.StatusBadRequest)
// 			ctx.WriteString(err.Error())
// 			return
// 		}

// 		resp, ok := obj.(Resulter)
// 		if !ok {
// 			// если тела ответа в виде JSON объекта не предполагается, то ожидается
// 			// что сам обработчик obj.Haldler() установит соответствующие статусы
// 			// и запишет в тело ответа соответствующие заголовки.
// 			// например обработчик отдачи файлов.
// 			return
// 		}

// 		ctx.SetContentType("application/json; charset=utf-8")

// 		if err := json.NewEncoder(ctx.Response.BodyWriter()).Encode(resp.Result()); err != nil {
// 			logger.Error().Str("errmsg", err.Error()).Msg("response write failed")
// 			return
// 		}
// 		logger.Info().Str("dur", time.Since(t).String()).Msg("completed")
// 	}
// }

// Doc возвращает описание входных и выходных параметров контроллера.
func (e *Endpoint) handleDescription(ctx Context) error {

	c := e.Controller()

	ctx.SetContentType([]byte("text/html; charset=utf-8"))

	_, err := ctx.BodyWriter().Write(e.genDescription(c))
	if err != nil {
		return errors.Catch(err).Msg("description response write failed").StatusCode(500)
	}
	return nil
}

func (e *Endpoint) genDescription(c Handler) []byte {
	s := "Endpoint description: " + e.Method + " -  " + e.Path
	if c == nil {
		s += "No handler"
	}

	if e.isPathParametrized {
		s += "\n" + goon.Sdump(c.(Paramer).Param())
	}

	if e.isRequestBodyExpected {
		s += "Body input: \n" + goon.Sdump(c.(Inputer).Input())
	}

	if e.isURLQueryExpected {
		s += "URL input\n" + goon.Sdump(c.(Inputer).Input())
	}

	if e.isResulter {
		s += "\n" + goon.Sdump(c.(Resulter).Result())
	}

	return []byte(s)
}

// TODO: сделать поддержку param не в виде структуры, а в виде одной переменной.
func decodeParams(ctx *fasthttp.RequestCtx, param interface{}) error {

	s := reflect.ValueOf(param).Elem()
	tof := s.Type()

	for i := 0; i < tof.NumField(); i++ {
		sf := s.Field(i)

		if sf.CanSet() == false {
			continue
		}

		tag := tof.Field(i).Tag.Get("param")
		if tag == "" {
			continue
		}

		val, ok := ctx.UserValue(tag).(string)
		if !ok {
			panic("non string param")
		}
		switch sf.Interface().(type) {
		case int, int8, int16, int32, int64:
			i, err := strconv.ParseInt(val, 10, 64)
			if err != nil {
				return errors.ValidationFailed(err.Error())
			}
			sf.SetInt(i)
		case uint, uint8, uint16, uint32, uint64:
			i, err := strconv.ParseUint(val, 10, 64)
			if err != nil {
				return errors.ValidationFailed(err.Error())
			}
			sf.SetUint(i)
		case string:
			sf.SetString(val)
		case bool:
			b, err := strconv.ParseBool(val)
			if err != nil {
				return errors.ValidationFailed(err.Error())
			}
			sf.SetBool(b)
		case float32, float64:
			f, err := strconv.ParseFloat(val, 64)
			if err != nil {
				return errors.ValidationFailed(err.Error())
			}
			sf.SetFloat(f)
		case []string:
			break
		default:
			return errors.ValidationFailed("unsuppoted go type").Set("tag", tag)
		}
	}
	return nil
}

func assign(val string, i interface{}) error {
	return nil
}

func decodeBody(ctx *fasthttp.RequestCtx, dest interface{}) error {
	buf := ctx.Request.Body()
	if len(buf) == 0 {
		return errors.InvalidRequestBody("empty request body. JSON expected")
	}
	return json.Unmarshal(buf, dest)
}

func decodeURLQuery(ctx *fasthttp.RequestCtx, input interface{}) error {

	s := reflect.ValueOf(input).Elem()
	tof := s.Type()

	for i := 0; i < tof.NumField(); i++ {
		sf := s.Field(i)

		if sf.CanSet() == false {
			continue
		}

		tof := tof.Field(i)

		tag := tof.Tag.Get("param")
		if tag == "" {
			continue
		}

		val := ctx.QueryArgs().Peek(tag)
		if val == nil {
			continue
		}

		if sf.Kind() == reflect.Ptr {
			if sf.IsNil() {
				sf.Set(reflect.New(sf.Type().Elem()))
			}
			sf = sf.Elem()
		}

		switch sf.Kind() {
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			k, err := strconv.ParseInt(string(val), 10, 64)
			if err != nil {
				return err
			}
			sf.SetInt(k)
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			k, err := strconv.ParseUint(string(val), 10, 64)
			if err != nil {
				return err
			}
			sf.SetUint(k)
		case reflect.String:
			sf.SetString(string(val))
		case reflect.Float64:
			k, err := strconv.ParseFloat(string(val), 64)
			if err != nil {
				return err
			}
			sf.SetFloat(k)
		case reflect.Float32:
			k, err := strconv.ParseFloat(string(val), 32)
			if err != nil {
				return err
			}
			sf.SetFloat(k)
		case reflect.Bool:
			b, err := strconv.ParseBool(string(val))
			if err != nil {
				return err
			}
			sf.SetBool(b)
		// case reflect. TODO date.Date YYYY-ММ-DD

		default:
			println("unsupported type:", string(val), sf.Kind())
		}
	}
	return nil
}

func (e *Endpoint) compile(v *Vatel) error {
	opath := e.Path
	e.Path = path.Join(v.uprefix, e.Path)
	e.auth = v.auth
	e.td = v.td
	e.pm = v.pm
	e.rd = v.rd

	if e.ResponseContentType != "" {
		e.responseContentType = []byte(e.ResponseContentType)
	} else {
		e.responseContentType = []byte("application/json; charset=utf-8")
	}

	if len(e.Perms) > 0 {
		if e.auth == nil && !v.authDisabled {
			return fmt.Errorf("endpoint %s %s requires calling SetAuthorizer() before", e.Method, opath)
		}
		if e.td == nil {
			return fmt.Errorf("endpoint %s %s requires calling SetTokenDecode() before", e.Method, opath)
		}

		if e.pm == nil {
			return fmt.Errorf("endpoint %s %s requires calling SetPermissionManager() before", e.Method, opath)
		}

		for i := range e.Perms {
			pb, ok := v.pm.PermissionBitPos(e.Perms[i])
			if !ok {
				return fmt.Errorf("endpoint %s %s mentioned unknown permission %s", e.Method, opath, e.Perms[i])
			}
			e.perms = append(e.perms, pb)
		}
	}
	c := e.Controller()

	// looking for "{ }"" in the path
	re, err := regexp.Compile(`(?s)\{(.*)\}`)
	if err != nil {
		return fmt.Errorf("endpoint %s %s cannot be parsed by regexp", e.Method, opath)
	}

	_, isParamer := c.(Paramer)
	pathHasParam := re.Match([]byte(e.Path))

	if !isParamer && pathHasParam {
		return fmt.Errorf("endpoint %s %s path has parameters, but controller does not implement Paramer", e.Method, opath)
	}

	if isParamer && !pathHasParam {
		return fmt.Errorf("endpoint %s %s path has no parameters, but controller implement Paramer", e.Method, opath)
	}
	e.isPathParametrized = isParamer

	_, isInputer := c.(Inputer)
	switch e.Method {
	case "GET", "DELETE":
		e.isURLQueryExpected = isInputer
	case "POST", "PUT", "PATCH":
		e.isRequestBodyExpected = isInputer
	default:
		return fmt.Errorf("endpoint %s has unknown HTTP method %s", opath, e.Method)
	}
	return nil
}
