// +build coprocess

//go:generate msgp
//msgp:ignore CoProcessor CoProcessMiddleware CoProcessMiddlewareConfig TykMiddleware

package main

/*
#include <stdio.h>

#include "coprocess/sds/sds.h"

#include "coprocess/api.h"

#include "coprocess/python/dispatcher.h"
#include "coprocess/python/binding.h"

*/
import "C"

import (
	"github.com/Sirupsen/logrus"
	"github.com/gorilla/context"
	"github.com/mitchellh/mapstructure"

	"github.com/TykTechnologies/tykcommon"
	"github.com/TykTechnologies/tyk/coprocess"

	"github.com/golang/protobuf/proto"

	"bytes"
	"errors"
	"io/ioutil"
	"net/http"
	"unsafe"
)

// config.EnableProcess will override this
var EnableCoProcess bool = false

var GlobalDispatcher CoProcessDispatcher

type CoProcessMiddleware struct {
	*TykMiddleware
	HookType coprocess.HookType
	HookName string
	MiddlewareDriver tykcommon.MiddlewareDriver
}

func CreateCoProcessMiddleware(hookName string, hookType coprocess.HookType, mwDriver tykcommon.MiddlewareDriver, tykMwSuper *TykMiddleware) func(http.Handler) http.Handler {
	dMiddleware := &CoProcessMiddleware{
		TykMiddleware: tykMwSuper,
		HookType:      hookType,
		HookName:	hookName,
		MiddlewareDriver: mwDriver,
	}

	return CreateMiddleware(dMiddleware, tykMwSuper)
}

func doCoprocessReload() {
	log.WithFields(logrus.Fields{
		"prefix": "coprocess",
	}).Info("Reloading middlewares")
	GlobalDispatcher.Reload()

}

type CoProcessor struct {
	HookType   coprocess.HookType
	Middleware *CoProcessMiddleware
}

func (c *CoProcessor) GetObjectFromRequest(r *http.Request) *coprocess.Object {

	defer r.Body.Close()
	originalBody, _ := ioutil.ReadAll(r.Body)

	var object *coprocess.Object
	var miniRequestObject *coprocess.MiniRequestObject

	miniRequestObject = &coprocess.MiniRequestObject{
		Headers: ProtoMap(r.Header),
		SetHeaders: make(map[string]string, 0),
		DeleteHeaders: make([]string, 0),
		Body: string(originalBody),
		Url: r.URL.Path,
		Params: ProtoMap(r.URL.Query()),
		AddParams: make(map[string]string),
		ExtendedParams: ProtoMap(nil),
		DeleteParams: make([]string, 0),
		ReturnOverrides: &coprocess.ReturnOverrides{-1, ""},
	}

	object = &coprocess.Object{
		Request: miniRequestObject,
		HookName: c.Middleware.HookName,
	}


	// If a middleware is set, take its HookType, otherwise override it with CoProcessor.HookType
	if c.Middleware != nil && c.HookType == 0 {
		c.HookType = c.Middleware.HookType
	}

	object.HookType = c.HookType

	object.Metadata = make(map[string]string, 0)
	object.Spec = make(map[string]string, 0)

	// object.Session = SessionState{}

	// Append spec data:
	if c.Middleware != nil {
		object.Spec = map[string]string{
			"OrgID": c.Middleware.TykMiddleware.Spec.OrgID,
			"APIID": c.Middleware.TykMiddleware.Spec.APIID,
		}
	}

	// Encode the session object (if not a pre-process & not a custom key check):
	if c.HookType != coprocess.HookType_Pre && c.HookType != coprocess.HookType_CustomKeyCheck {
		var session interface{}
		session = context.Get(r, SessionData)
		if session != nil {
			sessionState := session.(SessionState)
			object.Session = ProtoSessionState(sessionState)
		}
	}

	return object
}

func (c *CoProcessor) ObjectPostProcess(object *coprocess.Object, r *http.Request) {
	r.ContentLength = int64(len(object.Request.Body))
	r.Body = ioutil.NopCloser(bytes.NewBufferString(object.Request.Body))

	for _, dh := range object.Request.DeleteHeaders {
		r.Header.Del(dh)
	}

	for h, v := range object.Request.SetHeaders {
		r.Header.Set(h, v)
	}

	values := r.URL.Query()
	for _, k := range object.Request.DeleteParams {
		values.Del(k)
	}

	for p, v := range object.Request.AddParams {
		values.Set(p, v)
	}

	r.URL.RawQuery = values.Encode()
}

func (c *CoProcessor) Dispatch(object *coprocess.Object) *coprocess.Object {


	objectMsg, _ := proto.Marshal(object)

	objectMsgStr := string(objectMsg)

	var CObjectStr *C.char
	CObjectStr = C.CString(objectMsgStr)

	var objectPtr *C.struct_CoProcessMessage

	objectPtr = (*C.struct_CoProcessMessage)(C.malloc(C.size_t(unsafe.Sizeof(C.struct_CoProcessMessage{}))))
	objectPtr.p_data = unsafe.Pointer(CObjectStr)
	objectPtr.length = C.int(len(objectMsg))

	var newObjectPtr *C.struct_CoProcessMessage
	newObjectPtr = GlobalDispatcher.Dispatch(objectPtr)

	var newObjectBytes []byte
	newObjectBytes = C.GoBytes(newObjectPtr.p_data, newObjectPtr.length)

	newObject := &coprocess.Object{}
	proto.Unmarshal(newObjectBytes, newObject)

	C.free(unsafe.Pointer(CObjectStr))
	C.free(unsafe.Pointer(objectPtr))
	C.free(unsafe.Pointer(newObjectPtr))

	return newObject
}

type CoProcessDispatcher interface {
	Dispatch(*C.struct_CoProcessMessage) *C.struct_CoProcessMessage
	DispatchEvent([]byte)
	Reload()
}

func CoProcessInit() (err error) {
	if config.EnableCoProcess {
		GlobalDispatcher, err = NewCoProcessDispatcher()
		EnableCoProcess = true
	}
	return err
}

type CoProcessMiddlewareConfig struct {
	ConfigData map[string]string `mapstructure:"config_data" bson:"config_data" json:"config_data"`
}

type CoProcessObject struct {
	HookType string                     `json:"hook_type" msg:"hook_type"`
	Request  CoProcessMiniRequestObject `json:"request,omitempty" msg:"request"`
	Session  SessionState               `json:"session,omitempty" msg:"session"`
	Metadata map[string]string          `json:"metadata,omitempty" msg:"metadata"`
	Spec     map[string]string          `json:"spec,omitempty" msg:"spec"`
}

type CoProcessReturnOverrides struct {
	ResponseCode  int    `json:"response_code" msg:"response_code"`
	ResponseError string `json:"response_error" msg:"response_error"`
}
type CoProcessMiniRequestObject struct {
	Headers         map[string][]string      `msg:"headers"`
	SetHeaders      map[string]string        `msg:"set_headers"`
	DeleteHeaders   []string                 `msg:"delete_headers"`
	Body            string                   `msg:"body"`
	URL             string                   `msg:"url"`
	Params          map[string][]string      `msg:"params"`
	AddParams       map[string]string        `msg:"add_params"`
	ExtendedParams  map[string][]string      `msg:"extended_params"`
	DeleteParams    []string                 `msg:"delete_params"`
	ReturnOverrides CoProcessReturnOverrides `json:"return_overrides" msg:"return_overrides"`
}

// New lets you do any initialisations for the object can be done here
func (m *CoProcessMiddleware) New() {}

// GetConfig retrieves the configuration from the API config - we user mapstructure for this for simplicity
func (m *CoProcessMiddleware) GetConfig() (interface{}, error) {
	var thisModuleConfig CoProcessMiddlewareConfig

	err := mapstructure.Decode(m.TykMiddleware.Spec.APIDefinition.RawData, &thisModuleConfig)
	if err != nil {
		log.WithFields(logrus.Fields{
			"prefix": "jsvm",
		}).Error(err)
		return nil, err
	}

	return thisModuleConfig, nil
}

// ProcessRequest will run any checks on the request on the way through the system, return an error to have the chain fail
func (m *CoProcessMiddleware) ProcessRequest(w http.ResponseWriter, r *http.Request, configuration interface{}) (error, int) {
	log.WithFields(logrus.Fields{
		"prefix": "coprocess",
	}).Debug("CoProcess Request, HookType: ", m.HookType)

	if !EnableCoProcess {
		return nil, 200
	}

	// It's also possible to override the HookType:
	thisCoProcessor := CoProcessor{
		Middleware: m,
		// HookType: coprocess.PreHook,
	}

	object := thisCoProcessor.GetObjectFromRequest(r)

	returnObject := thisCoProcessor.Dispatch(object)

	thisCoProcessor.ObjectPostProcess(returnObject, r)

	authHeaderValue := returnObject.Metadata["token"]

	if returnObject.Request.ReturnOverrides.ResponseCode > 400 {

		log.WithFields(logrus.Fields{
			"path":   r.URL.Path,
			"origin": GetIPFromRequest(r),
			"key":    authHeaderValue,
		}).Info("Attempted access with invalid key.")

		// Fire Authfailed Event
		AuthFailed(m.TykMiddleware, r, authHeaderValue)

		// Report in health check
		ReportHealthCheckValue(m.Spec.Health, KeyFailure, "1")

		return errors.New("Key not authorised"), int(returnObject.Request.ReturnOverrides.ResponseCode)
	}

	if m.HookType == coprocess.HookType_CustomKeyCheck {
		if returnObject.Session != nil {
			// context.Set(r, SessionData, ToTykSession(returnObject.Session))
			context.Set(r, SessionData, TykSessionState(returnObject.Session) )
			context.Set(r, AuthHeaderValue, authHeaderValue)
		}
	}

	// context.GetOk(r, SessionData)

	return nil, 200
}

//export CoProcess_Log
func CoProcess_Log(CMessage *C.char, CLogLevel *C.char) {
	var message, logLevel string
	message = C.GoString(CMessage)
	logLevel = C.GoString(CLogLevel)

	switch logLevel {
	case "debug":
		log.WithFields(logrus.Fields{
			"prefix": CoProcessName,
		}).Debug(message)
	case "error":
		log.WithFields(logrus.Fields{
			"prefix": CoProcessName,
		}).Error(message)
	case "warning":
		log.WithFields(logrus.Fields{
			"prefix": CoProcessName,
		}).Warning(message)
	default:
		log.WithFields(logrus.Fields{
			"prefix": CoProcessName,
		}).Info(message)
	}
}
