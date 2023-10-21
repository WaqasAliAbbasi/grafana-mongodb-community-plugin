package plugin

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/data"

	"go.mongodb.org/mongo-driver/bson"
	bsonPrim "go.mongodb.org/mongo-driver/bson/primitive"
)

func ToGrafanaValue(value interface{}) (interface{}, data.FieldType, error) {
	// Only handles types explicitly referenced as being returned from bson.Unmarshal
	// https://pkg.go.dev/go.mongodb.org/mongo-driver@v1.11.1/bson#hdr-Native_Go_Types
	// notably, this does not deal with pointer types, like *float64

	// 19
	if value == nil {
		return nil, data.FieldTypeUnknown, nil
	}
	switch v := value.(type) {
	case int32, int64, float64, string, bool: // 1-5
		return value, data.FieldTypeFor(value), nil
	case bsonPrim.A, []interface{}: // 6
		// []interface{} isn't documented, but can be observed to be returned
		// MarshalExtJSON doesn't accept arrays for whatever reason
		// https://github.com/mongodb/mongo-go-driver/blob/v1/docs/common-issues.md#writexxx-can-only-write-while-positioned-on-a-element-or-value-but-is-positioned-on-a-toplevel
		bytes, err := bson.MarshalExtJSON(bsonPrim.M{"Value": value}, false, false)
		if err != nil {
			return nil, data.FieldTypeUnknown, err
		}

		/*
			// This is the "safe" but slow way,
			// We have to do this dance where we marshal it to JSON, unmarshall it back,
			// extract the data we want, and then re-marshal just that
			var roundTrip struct{ Value interface{} }
			err = json.Unmarshal(bytes, &roundTrip)
			if err != nil {
				return nil, data.FieldTypeUnknown, err
			}
			bytes, err = json.Marshal(roundTrip.Value)
			if err != nil {
				return nil, data.FieldTypeUnknown, err
			}
		*/

		// This is the fast but dangerous way.
		// In theory, it should never produce anything except {"Value":list_goes_here},
		// so this should never fail, and it passes the test, but this isn't guaranteed
		bytes = bytes[len([]byte(`{"Value":`)):]
		bytes = bytes[:len(bytes)-len([]byte("}"))]

		return json.RawMessage(bytes), data.FieldTypeJSON, err
	case bsonPrim.D, bsonPrim.M, map[string]interface{}: // 7
		// map[string]interface{} isn't documented, but can be observed to be returned
		bytes, err := bson.MarshalExtJSON(value, false, false)
		if err != nil {
			return nil, data.FieldTypeUnknown, err
		}
		return json.RawMessage(bytes), data.FieldTypeJSON, err
	case bsonPrim.ObjectID: // 8
		bytes := [12]byte(v)
		return hex.EncodeToString(bytes[:]), data.FieldTypeString, nil
	case bsonPrim.DateTime: // 9
		return v.Time(), data.FieldTypeTime, nil
	case bsonPrim.Binary: // 10
		return hex.EncodeToString(v.Data), data.FieldTypeString, nil
	case bsonPrim.Regex: // 11
		return fmt.Sprintf("%s", v.Pattern), data.FieldTypeString, nil
	case bsonPrim.JavaScript: // 12
		return string(v), data.FieldTypeString, nil
	case bsonPrim.CodeWithScope: // 13
		return string(v.Code), data.FieldTypeString, nil
	case bsonPrim.Timestamp: // 14
		return time.Unix(int64(v.T), 0), data.FieldTypeTime, nil
	case bsonPrim.Decimal128: // 15
		f, err := strconv.ParseFloat(v.String(), 64)
		return f, data.FieldTypeFloat64, err
	case bsonPrim.MinKey, bsonPrim.MaxKey: // 16-17
		return fmt.Sprintf("%#v", v), data.FieldTypeString, nil
	case bsonPrim.Undefined: // 18
		return nil, data.FieldTypeUnknown, nil
	// 19: See above
	case bsonPrim.DBPointer: // 20
		return fmt.Sprintf("%#v", v), data.FieldTypeString, nil
	case bsonPrim.Symbol: // 21
		return string(v), data.FieldTypeString, nil
	}
	return nil, data.FieldTypeUnknown, fmt.Errorf("Got value with a type not expected to be generated by BSON: %#v (%s)", value, reflect.ValueOf(value).Type())
}

func convertValue(value interface{}, nullable bool) (interface{}, data.FieldType, error) {
	converted, type_, err := ToGrafanaValue(value)
	if err != nil {
		return nil, type_, err
	}
	if converted == nil {
		return nil, type_, nil
	}
	if !nullable {
		return converted, type_, nil
	}

	// Adding e.g. a float64 to a frame of *float64 is not handled seamlessly,
	// we have do it manually
	// We can't just do valueValueValue.Addr().Interface(), as scalar's aren't addressable
	convertedValue := reflect.ValueOf(converted)
	convertedPtr := reflect.New(convertedValue.Type())
	convertedPtr.Elem().Set(convertedValue)
	return convertedPtr.Interface(), type_.NullableType(), nil
}