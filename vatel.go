package vatel

import (
	"sort"
	"strings"

	"github.com/fasthttp/router"
	"github.com/rs/zerolog"
	"github.com/valyala/fasthttp"
)

// TokenPayloader is the interface that wraps access methods
// to JWT payload parts.
//
// User returns value of user attribute from the token.
//
// Perms returns bitset array with user role's permissions.
type TokenPayloader interface {
	User() int
	Login() string
	Role() int
	Perms() []byte
	Extra() interface{}
	Debug() bool
}

// Tokener is the interface that wraps methods SystemPayload and UserPayload.
//
// SystemPayload returns JWT part related to JWT itself.
//
// UserPayload returns an object that represents JWT payload specified by user.
type Tokener interface {
	SystemPayload() map[string]interface{}
	ApplicationPayload() TokenPayloader
}

// Authorizer is the interface that wraps IsAllowed method.
//
// Authorizer accepts request permissions and permissions required by endpoint.
// Returns true if all endpointPerms are inside requestPerms.
type Authorizer interface {
	IsAllowed(requestPerms []byte, endpointPerms ...uint) (bool, error)
}

type RequestDebugger interface {
	IsDebugRequired(TokenPayloader) (in, out bool)
}

// PermissionManager ...
type PermissionManager interface {
	PermissionBitPos(perm string) (uint, bool)
}

// RevokeTokenChecker is the interface what wraps a single method IsTokenRevoked.
//
// IsTokenRevoked returns true if access token was revoked.
type RevokeTokenChecker interface {
	IsTokenRevoked(accessToken string) (bool, error)
}

// TokenDecoder is the interface what wraps a single method Decode.
//
// TokenDecoder decodes token and returns object Tokener.
type TokenDecoder interface {
	Decode(encodedToken []byte) (Tokener, error)
}

type Logger interface {
	Log()
}

// Vatel holds
type Vatel struct {
	ep      []Endpoint
	params  map[string]string
	uprefix string
	auth    Authorizer
	td      TokenDecoder
	pm      PermissionManager
	rd      RequestDebugger
	rtc     RevokeTokenChecker

	mdw []func(Context) error
	nfh func(Context) error

	authDisabled bool
}

// NewVatel returns new instance of Vatel.
func NewVatel(urlprefix string) *Vatel {
	v := Vatel{params: map[string]string{}, uprefix: urlprefix}
	v.ep = []Endpoint{{Method: "GET", Path: "/", Controller: func() Handler { return &tocController{s: &v} }}}
	return &v
}

// SetAuthorizer assigns authorization implementation.
// If Authorizer is not assigned, all Endpoint's Perms will be ignored.
func (v *Vatel) SetAuthorizer(a Authorizer) {
	v.auth = a
}

func (v *Vatel) SetRevokeTokenChecker(rtc RevokeTokenChecker) {
	v.rtc = rtc
}

func (v *Vatel) DisableAuthorizer() {
	v.authDisabled = true
}

// SetPermissionManager assigns permission manager implementation.
func (v *Vatel) SetPermissionManager(pm PermissionManager) {
	v.pm = pm
}

// SetRequestDebugger assigns request debugger implementation.
func (v *Vatel) SetRequestDebugger(rd RequestDebugger) {
	v.rd = rd
}

// SetTokenDecoder assigns session token decoder.
func (v *Vatel) SetTokenDecoder(tp TokenDecoder) {
	v.td = tp
}

// Add add endpoints to the list.
//
// The method does not check Endpoint for correctes and uqiqueness here.
// Paths validation implemented by method BuildHandlers.
func (v *Vatel) Add(e ...Endpointer) {
	for i := range e {
		v.ep = append(v.ep, e[i].Endpoints()...)
	}
}

// Set assigns related
func (v *Vatel) Set(key, value string) {
	v.params[key] = value
}

// Endpoints returns all registered endpoints.
func (v *Vatel) Endpoints() []Endpoint {
	return v.ep
}

// MustBuildHandlers initializes http muxer with rules by converting []Endpoint
// added before. Panics if:
// 	- there are Perms but SetAuthorizer or SetTokenDecoder were not called.
// 	-
func (v *Vatel) MustBuildHandlers(mux *router.Router, l *zerolog.Logger) {
	if err := v.buildHandlers(mux, l); err != nil {
		panic(err.Error())
	}
}

// BuildHandlers initializes http muxer with rules by converting []Endpoint
// added before.
func (v *Vatel) BuildHandlers(mux *router.Router, l *zerolog.Logger) error {
	return v.buildHandlers(mux, l)
}

func (v *Vatel) buildHandlers(mux *router.Router, l *zerolog.Logger) error {

	for i := range v.ep {
		v.ep[i].Method = strings.ToUpper(v.ep[i].Method)
	}

	sort.Slice(v.ep, func(i, j int) bool {
		if v.ep[i].Path == v.ep[j].Path {
			return v.ep[i].Method < v.ep[j].Method
		}
		return v.ep[i].Path < v.ep[j].Path
	})

	for i := range v.ep {
		e := &v.ep[i]
		if err := e.compile(v); err != nil {
			return err
		}

		logger := l.With().Str("method", e.Method).Str("path", e.Path).Logger()
		mux.Handle(e.Method, e.Path, fasthttp.CompressHandler(e.handler(&logger)))
		logger.Info().Msg("handler registered")
	}
	return nil
}

type Dater interface {
	Parse(string) (interface{}, error)
	Set(interface{})
}

func (v *Vatel) AddMiddleware(f ...func(Context) error) {
	v.mdw = append(v.mdw, f...)
}

func (v *Vatel) NotFoundHandler(f func(Context) error) {
	v.nfh = f
}
