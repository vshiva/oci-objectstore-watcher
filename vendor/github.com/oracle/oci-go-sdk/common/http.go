// Package common Copyright (c) 2016, 2017, 2018 Oracle and/or its affiliates. All rights reserved.
package common

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"path"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"time"
)

///////////////////////////////////////////////////////////////////////////////////////////////////////////////////////
///////////////////////////////////////////////////////////////////////////////////////////////////////////////////////
//Request Marshaling
////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////
////////////////////////////////////////////////////////////////////////////////////////////////////////////////

func isNil(v reflect.Value) bool {
	return v.Kind() == reflect.Ptr && v.IsNil()
}

// Returns the string representation of a reflect.Value
// Only transforms primitive values
func toStringValue(v reflect.Value, field reflect.StructField) (string, error) {
	if v.Kind() == reflect.Ptr {
		if v.IsNil() {
			return "", fmt.Errorf("can not marshal a nil pointer")
		}
		v = v.Elem()
	}

	if v.Type() == timeType {
		t := v.Interface().(SDKTime)
		return formatTime(t), nil
	}

	switch v.Kind() {
	case reflect.Bool:
		return strconv.FormatBool(v.Bool()), nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return strconv.FormatInt(v.Int(), 10), nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return strconv.FormatUint(v.Uint(), 10), nil
	case reflect.String:
		return v.String(), nil
	case reflect.Float32:
		return strconv.FormatFloat(v.Float(), 'f', 6, 32), nil
	case reflect.Float64:
		return strconv.FormatFloat(v.Float(), 'f', 6, 64), nil
	default:
		return "", fmt.Errorf("marshaling structure to a http.Request does not support field named: %s of type: %v",
			field.Name, v.Type().String())
	}
}

func addBinaryBody(request *http.Request, value reflect.Value) (e error) {
	readCloser, ok := value.Interface().(io.ReadCloser)
	if !ok {
		e = fmt.Errorf("body of the request needs to be an io.ReadCloser interface. Can not marshal body of binary request")
		return
	}

	request.Body = readCloser

	//Set the default content type to application/octet-stream if not set
	if request.Header.Get("Content-Type") == "" {
		request.Header.Set("Content-Type", "application/octet-stream")
	}
	return nil
}

func addToBody(request *http.Request, value reflect.Value, field reflect.StructField) (e error) {
	Debugln("Marshaling to body from field:", field.Name)
	if request.Body != nil {
		Logln("The body of the request is already set. Structure: ", field.Name, " will overwrite it")
	}
	tag := field.Tag
	encoding := tag.Get("encoding")

	if encoding == "binary" {
		return addBinaryBody(request, value)
	}

	marshaled, e := json.Marshal(value.Interface())
	if e != nil {
		return
	}
	Debugf("Marshaled body is: %s", string(marshaled))
	bodyBytes := bytes.NewReader(marshaled)
	request.ContentLength = int64(bodyBytes.Len())
	request.Header.Set("Content-Length", strconv.FormatInt(request.ContentLength, 10))
	request.Header.Set("Content-Type", "application/json")
	request.Body = ioutil.NopCloser(bodyBytes)
	request.GetBody = func() (io.ReadCloser, error) {
		return ioutil.NopCloser(bodyBytes), nil
	}
	return
}

func addToQuery(request *http.Request, value reflect.Value, field reflect.StructField) (e error) {
	Debugln("Marshaling to query from field:", field.Name)
	if request.URL == nil {
		request.URL = &url.URL{}
	}
	query := request.URL.Query()
	var queryParameterValue, queryParameterName string

	if queryParameterName = field.Tag.Get("name"); queryParameterName == "" {
		return fmt.Errorf("marshaling request to a query requires the 'name' tag for field: %s ", field.Name)
	}

	mandatory, _ := strconv.ParseBool(strings.ToLower(field.Tag.Get("mandatory")))

	//If mandatory and nil. Error out
	if mandatory && isNil(value) {
		return fmt.Errorf("marshaling request to a header requires not nil pointer for field: %s", field.Name)
	}

	//if not mandatory and nil. Omit
	if !mandatory && isNil(value) {
		Debugf("Query parameter value is not mandatory and is nil pointer in field: %s. Skipping header", field.Name)
		return
	}

	if queryParameterValue, e = toStringValue(value, field); e != nil {
		return
	}

	//check for tag "omitEmpty", this is done to accomodate unset fields that do not
	//support an empty string: enums in query params
	if omitEmpty, present := field.Tag.Lookup("omitEmpty"); present {
		omitEmptyBool, _ := strconv.ParseBool(strings.ToLower(omitEmpty))
		if queryParameterValue != "" || !omitEmptyBool {
			query.Set(queryParameterName, queryParameterValue)
		} else {
			Debugf("Omitting %s, is empty and omitEmpty tag is set", field.Name)
		}
	} else {
		query.Set(queryParameterName, queryParameterValue)
	}

	request.URL.RawQuery = query.Encode()
	return
}

// Adds to the path of the url in the order they appear in the structure
func addToPath(request *http.Request, value reflect.Value, field reflect.StructField) (e error) {
	var additionalURLPathPart string
	if additionalURLPathPart, e = toStringValue(value, field); e != nil {
		return fmt.Errorf("can not marshal to path in request for field %s. Due to %s", field.Name, e.Error())
	}

	if request.URL == nil {
		request.URL = &url.URL{}
		request.URL.Path = ""
	}
	var currentURLPath = request.URL.Path

	var templatedPathRegex, _ = regexp.Compile(".*{.+}.*")
	if !templatedPathRegex.MatchString(currentURLPath) {
		Debugln("Marshaling request to path by appending field:", field.Name)
		allPath := []string{currentURLPath, additionalURLPathPart}
		newPath := strings.Join(allPath, "/")
		request.URL.Path = path.Clean(newPath)
	} else {
		var fieldName string
		if fieldName = field.Tag.Get("name"); fieldName == "" {
			e = fmt.Errorf("marshaling request to path name and template requires a 'name' tag for field: %s", field.Name)
			return
		}
		urlTemplate := currentURLPath
		Debugln("Marshaling to path from field:", field.Name, "in template:", urlTemplate)
		request.URL.Path = path.Clean(strings.Replace(urlTemplate, "{"+fieldName+"}", additionalURLPathPart, -1))
	}
	return
}

func setWellKnownHeaders(request *http.Request, headerName, headerValue string) (e error) {
	switch strings.ToLower(headerName) {
	case "content-length":
		var len int
		len, e = strconv.Atoi(headerValue)
		if e != nil {
			return
		}
		request.ContentLength = int64(len)
	}
	return nil
}

func addToHeader(request *http.Request, value reflect.Value, field reflect.StructField) (e error) {
	Debugln("Marshaling to header from field:", field.Name)
	if request.Header == nil {
		request.Header = http.Header{}
	}

	var headerName, headerValue string
	if headerName = field.Tag.Get("name"); headerName == "" {
		return fmt.Errorf("marshaling request to a header requires the 'name' tag for field: %s", field.Name)
	}

	mandatory, _ := strconv.ParseBool(strings.ToLower(field.Tag.Get("mandatory")))
	//If mandatory and nil. Error out
	if mandatory && isNil(value) {
		return fmt.Errorf("marshaling request to a header requires not nil pointer for field: %s", field.Name)
	}

	//if not mandatory and nil. Omit
	if !mandatory && isNil(value) {
		Debugf("Header value is not mandatory and is nil pointer in field: %s. Skipping header", field.Name)
		return
	}

	//Otherwise get value and set header
	if headerValue, e = toStringValue(value, field); e != nil {
		return
	}

	if e = setWellKnownHeaders(request, headerName, headerValue); e != nil {
		return
	}

	request.Header.Set(headerName, headerValue)
	return
}

// Header collection is a map of string to string that gets rendered as individual headers with a given prefix
func addToHeaderCollection(request *http.Request, value reflect.Value, field reflect.StructField) (e error) {
	Debugln("Marshaling to header-collection from field:", field.Name)
	if request.Header == nil {
		request.Header = http.Header{}
	}

	var headerPrefix string
	if headerPrefix = field.Tag.Get("prefix"); headerPrefix == "" {
		return fmt.Errorf("marshaling request to a header requires the 'prefix' tag for field: %s", field.Name)
	}

	mandatory, _ := strconv.ParseBool(strings.ToLower(field.Tag.Get("mandatory")))
	//If mandatory and nil. Error out
	if mandatory && isNil(value) {
		return fmt.Errorf("marshaling request to a header requires not nil pointer for field: %s", field.Name)
	}

	//if not mandatory and nil. Omit
	if !mandatory && isNil(value) {
		Debugf("Header value is not mandatory and is nil pointer in field: %s. Skipping header", field.Name)
		return
	}

	//cast to map
	headerValues, ok := value.Interface().(map[string]string)
	if !ok {
		e = fmt.Errorf("header fields need to be of type map[string]string")
		return
	}

	for k, v := range headerValues {
		headerName := fmt.Sprintf("%s%s", headerPrefix, k)
		request.Header.Set(headerName, v)
	}
	return
}

// Makes sure the incoming structure is able to be marshalled
// to a request
func checkForValidRequestStruct(s interface{}) (*reflect.Value, error) {
	val := reflect.ValueOf(s)
	for val.Kind() == reflect.Ptr {
		if val.IsNil() {
			return nil, fmt.Errorf("can not marshal to request a pointer to structure")
		}
		val = val.Elem()
	}

	if s == nil {
		return nil, fmt.Errorf("can not marshal to request a nil structure")
	}

	if val.Kind() != reflect.Struct {
		return nil, fmt.Errorf("can not marshal to request, expects struct input. Got %v", val.Kind())
	}

	return &val, nil
}

// Populates the parts of a request by reading tags in the passed structure
// nested structs are followed recursively depth-first.
func structToRequestPart(request *http.Request, val reflect.Value) (err error) {
	typ := val.Type()
	for i := 0; i < typ.NumField(); i++ {
		if err != nil {
			return
		}

		sf := typ.Field(i)
		//unexported
		if sf.PkgPath != "" && !sf.Anonymous {
			continue
		}

		sv := val.Field(i)
		tag := sf.Tag.Get("contributesTo")
		switch tag {
		case "header":
			err = addToHeader(request, sv, sf)
		case "header-collection":
			err = addToHeaderCollection(request, sv, sf)
		case "path":
			err = addToPath(request, sv, sf)
		case "query":
			err = addToQuery(request, sv, sf)
		case "body":
			err = addToBody(request, sv, sf)
		case "":
			Debugln(sf.Name, "does not contain contributes tag. Skipping.")
		default:
			err = fmt.Errorf("can not marshal field: %s. It needs to contain valid contributesTo tag", sf.Name)
		}
	}

	//If headers are and the content type was not set, we default to application/json
	if request.Header != nil && request.Header.Get("Content-Type") == "" {
		request.Header.Set("Content-Type", "application/json")
	}

	return
}

// HTTPRequestMarshaller marshals a structure to an http request using tag values in the struct
// The marshaller tag should like the following
// type A struct {
// 		 ANumber string `contributesTo="query" name="number"`
//		 TheBody `contributesTo="body"`
// }
// where the contributesTo tag can be: header, path, query, body
// and the 'name' tag is the name of the value used in the http request(not applicable for path)
// If path is specified as part of the tag, the values are appened to the url path
// in the order they appear in the structure
// The current implementation only supports primitive types, except for the body tag, which needs a struct type.
// The body of a request will be marshaled using the tags of the structure
func HTTPRequestMarshaller(requestStruct interface{}, httpRequest *http.Request) (err error) {
	var val *reflect.Value
	if val, err = checkForValidRequestStruct(requestStruct); err != nil {
		return
	}

	Debugln("Marshaling to Request:", val.Type().Name())
	err = structToRequestPart(httpRequest, *val)
	return
}

// MakeDefaultHTTPRequest creates the basic http request with the necessary headers set
func MakeDefaultHTTPRequest(method, path string) (httpRequest http.Request) {
	httpRequest = http.Request{
		Proto:      "HTTP/1.1",
		ProtoMajor: 1,
		ProtoMinor: 1,
		Header:     make(http.Header),
		URL:        &url.URL{},
	}

	httpRequest.Header.Set("Content-Length", "0")
	httpRequest.Header.Set("Date", time.Now().UTC().Format(http.TimeFormat))
	httpRequest.Header.Set("Opc-Client-Info", strings.Join([]string{defaultSDKMarker, Version()}, "/"))
	httpRequest.Header.Set("Accept", "*/*")
	httpRequest.Method = method
	httpRequest.URL.Path = path
	return
}

// MakeDefaultHTTPRequestWithTaggedStruct creates an http request from an struct with tagged fields, see HTTPRequestMarshaller
// for more information
func MakeDefaultHTTPRequestWithTaggedStruct(method, path string, requestStruct interface{}) (httpRequest http.Request, err error) {
	httpRequest = MakeDefaultHTTPRequest(method, path)
	err = HTTPRequestMarshaller(requestStruct, &httpRequest)
	return
}

///////////////////////////////////////////////////////////////////////////////////////////////////////////////////////
///////////////////////////////////////////////////////////////////////////////////////////////////////////////////////
//Request UnMarshaling
////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////
////////////////////////////////////////////////////////////////////////////////////////////////////////////////

// Makes sure the incoming structure is able to be unmarshaled
// to a request
func checkForValidResponseStruct(s interface{}) (*reflect.Value, error) {
	val := reflect.ValueOf(s)
	for val.Kind() == reflect.Ptr {
		if val.IsNil() {
			return nil, fmt.Errorf("can not unmarshal to response a pointer to nil structure")
		}
		val = val.Elem()
	}

	if s == nil {
		return nil, fmt.Errorf("can not unmarshal to response a nil structure")
	}

	if val.Kind() != reflect.Struct {
		return nil, fmt.Errorf("can not unmarshal to response, expects struct input. Got %v", val.Kind())
	}

	return &val, nil
}

func intSizeFromKind(kind reflect.Kind) int {
	switch kind {
	case reflect.Int8, reflect.Uint8:
		return 8
	case reflect.Int16, reflect.Uint16:
		return 16
	case reflect.Int32, reflect.Uint32:
		return 32
	case reflect.Int64, reflect.Uint64:
		return 64
	case reflect.Int, reflect.Uint:
		return strconv.IntSize
	default:
		Debugln("The type is not valid: %v. Returing int size for arch", kind.String())
		return strconv.IntSize
	}

}

func analyzeValue(stringValue string, kind reflect.Kind, field reflect.StructField) (val reflect.Value, valPointer reflect.Value, err error) {
	switch kind {
	case timeType.Kind():
		var t time.Time
		t, err = tryParsingTimeWithValidFormatsForHeaders([]byte(stringValue), field.Name)
		if err != nil {
			return
		}
		sdkTime := sdkTimeFromTime(t)
		val = reflect.ValueOf(sdkTime)
		valPointer = reflect.ValueOf(&sdkTime)
		return
	case reflect.Bool:
		var bVal bool
		if bVal, err = strconv.ParseBool(stringValue); err != nil {
			return
		}
		val = reflect.ValueOf(bVal)
		valPointer = reflect.ValueOf(&bVal)
		return
	case reflect.Int:
		size := intSizeFromKind(kind)
		var iVal int64
		if iVal, err = strconv.ParseInt(stringValue, 10, size); err != nil {
			return
		}
		var iiVal int
		iiVal = int(iVal)
		val = reflect.ValueOf(iiVal)
		valPointer = reflect.ValueOf(&iiVal)
		return
	case reflect.Int64:
		size := intSizeFromKind(kind)
		var iVal int64
		if iVal, err = strconv.ParseInt(stringValue, 10, size); err != nil {
			return
		}
		val = reflect.ValueOf(iVal)
		valPointer = reflect.ValueOf(&iVal)
		return
	case reflect.Uint:
		size := intSizeFromKind(kind)
		var iVal uint64
		if iVal, err = strconv.ParseUint(stringValue, 10, size); err != nil {
			return
		}
		var uiVal uint
		uiVal = uint(iVal)
		val = reflect.ValueOf(uiVal)
		valPointer = reflect.ValueOf(&uiVal)
		return
	case reflect.String:
		val = reflect.ValueOf(stringValue)
		valPointer = reflect.ValueOf(&stringValue)
	case reflect.Float32:
		var fVal float64
		if fVal, err = strconv.ParseFloat(stringValue, 32); err != nil {
			return
		}
		var ffVal float32
		ffVal = float32(fVal)
		val = reflect.ValueOf(ffVal)
		valPointer = reflect.ValueOf(&ffVal)
		return
	case reflect.Float64:
		var fVal float64
		if fVal, err = strconv.ParseFloat(stringValue, 64); err != nil {
			return
		}
		val = reflect.ValueOf(fVal)
		valPointer = reflect.ValueOf(&fVal)
		return
	default:
		err = fmt.Errorf("value for kind: %s not supported", kind)
	}
	return
}

// Sets the field of a struct, with the appropiate value of the string
// Only sets basic types
func fromStringValue(newValue string, val *reflect.Value, field reflect.StructField) (err error) {

	if !val.CanSet() {
		err = fmt.Errorf("can not set field name: %s of type: %v", field.Name, val.Type().String())
		return
	}

	kind := val.Kind()
	isPointer := false
	if val.Kind() == reflect.Ptr {
		isPointer = true
		kind = field.Type.Elem().Kind()
	}

	value, valPtr, err := analyzeValue(newValue, kind, field)
	if err != nil {
		return
	}
	if !isPointer {
		val.Set(value)
	} else {
		val.Set(valPtr)
	}
	return
}

// PolymorphicJSONUnmarshaler is the interface to unmarshal polymorphic json payloads
type PolymorphicJSONUnmarshaler interface {
	UnmarshalPolymorphicJSON(data []byte) (interface{}, error)
}

func valueFromPolymorphicJSON(content []byte, unmarshaler PolymorphicJSONUnmarshaler) (val interface{}, err error) {
	err = json.Unmarshal(content, unmarshaler)
	if err != nil {
		return
	}
	val, err = unmarshaler.UnmarshalPolymorphicJSON(content)
	return
}

func valueFromJSONBody(response *http.Response, value *reflect.Value, unmarshaler PolymorphicJSONUnmarshaler) (val interface{}, err error) {
	//Consumes the body, consider implementing it
	//without body consumption
	var content []byte
	content, err = ioutil.ReadAll(response.Body)
	if err != nil {
		return
	}

	if unmarshaler != nil {
		val, err = valueFromPolymorphicJSON(content, unmarshaler)
		return
	}

	val = reflect.New(value.Type()).Interface()
	err = json.Unmarshal(content, &val)
	return
}

func addFromBody(response *http.Response, value *reflect.Value, field reflect.StructField, unmarshaler PolymorphicJSONUnmarshaler) (err error) {
	Debugln("Unmarshaling from body to field:", field.Name)
	if response.Body == nil {
		Debugln("Unmarshaling body skipped due to nil body content for field: ", field.Name)
		return nil
	}

	tag := field.Tag
	encoding := tag.Get("encoding")
	var iVal interface{}
	switch encoding {
	case "binary":
		value.Set(reflect.ValueOf(response.Body))
		return
	case "": //If the encoding is not set. we'll decode with json
		iVal, err = valueFromJSONBody(response, value, unmarshaler)
		if err != nil {
			return
		}

		newVal := reflect.ValueOf(iVal)
		if newVal.Kind() == reflect.Ptr {
			newVal = newVal.Elem()
		}
		value.Set(newVal)
		return
	default:
		err = fmt.Errorf("Encoding: %s is invalid. Can not unmarshal body", encoding)
		return
	}
}

func addFromHeader(response *http.Response, value *reflect.Value, field reflect.StructField) (err error) {
	Debugln("Unmarshaling from header to field:", field.Name)
	var headerName string
	if headerName = field.Tag.Get("name"); headerName == "" {
		return fmt.Errorf("unmarshaling response to a header requires the 'name' tag for field: %s", field.Name)
	}

	headerValue := response.Header.Get(headerName)
	if headerValue == "" {
		Debugf("Unmarshalling did not find header with name:%s", headerName)
		return nil
	}

	if err = fromStringValue(headerValue, value, field); err != nil {
		return fmt.Errorf("unmarshaling response to a header failed for field %s, due to %s", field.Name,
			err.Error())
	}
	return
}

func addFromHeaderCollection(response *http.Response, value *reflect.Value, field reflect.StructField) error {
	Debugln("Unmarshaling from header-collection to field:", field.Name)
	var headerPrefix string
	if headerPrefix = field.Tag.Get("prefix"); headerPrefix == "" {
		return fmt.Errorf("Unmarshaling response to a header-collection requires the 'prefix' tag for field: %s", field.Name)
	}

	mapCollection := make(map[string]string)
	for name, value := range response.Header {
		if strings.HasPrefix(name, headerPrefix) {
			headerNoPrefix := strings.TrimPrefix(name, headerPrefix)
			mapCollection[headerNoPrefix] = value[0]
		}
	}

	value.Set(reflect.ValueOf(mapCollection))
	return nil
}

// Populates a struct from parts of a request by reading tags of the struct
func responseToStruct(response *http.Response, val *reflect.Value, unmarshaler PolymorphicJSONUnmarshaler) (err error) {
	typ := val.Type()
	for i := 0; i < typ.NumField(); i++ {
		if err != nil {
			return
		}

		sf := typ.Field(i)

		//unexported
		if sf.PkgPath != "" {
			continue
		}

		sv := val.Field(i)
		tag := sf.Tag.Get("presentIn")
		switch tag {
		case "header":
			err = addFromHeader(response, &sv, sf)
		case "header-collection":
			err = addFromHeaderCollection(response, &sv, sf)
		case "body":
			err = addFromBody(response, &sv, sf, unmarshaler)
		case "":
			Debugln(sf.Name, "does not contain presentIn tag. Skipping")
		default:
			err = fmt.Errorf("can not unmarshal field: %s. It needs to contain valid presentIn tag", sf.Name)
		}
	}
	return
}

// UnmarshalResponse hydrates the fields of a struct with the values of a http response, guided
// by the field tags. The directive tag is "presentIn" and it can be either
//  - "header": Will look for the header tagged as "name" in the headers of the struct and set it value to that
//  - "body": It will try to marshal the body from a json string to a struct tagged with 'presentIn: "body"'.
// Further this method will consume the body it should be safe to close it after this function
// Notice the current implementation only supports native types:int, strings, floats, bool as the field types
func UnmarshalResponse(httpResponse *http.Response, responseStruct interface{}) (err error) {

	var val *reflect.Value
	if val, err = checkForValidResponseStruct(responseStruct); err != nil {
		return
	}

	if err = responseToStruct(httpResponse, val, nil); err != nil {
		return
	}

	return nil
}

// UnmarshalResponseWithPolymorphicBody similar to UnmarshalResponse but assumes the body of the response
// contains polymorphic json. This function will use the unmarshaler argument to unmarshal json content
func UnmarshalResponseWithPolymorphicBody(httpResponse *http.Response, responseStruct interface{}, unmarshaler PolymorphicJSONUnmarshaler) (err error) {

	var val *reflect.Value
	if val, err = checkForValidResponseStruct(responseStruct); err != nil {
		return
	}

	if err = responseToStruct(httpResponse, val, unmarshaler); err != nil {
		return
	}

	return nil
}