package jsonmask

import (
	"reflect"
	"strconv"
	"strings"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// Field holds structure's field metadata.
type Field struct {
	name  string
	tag   string
	child []Field
}

// Field holds field of a single structure.
type Fields []Field

type RawJsonMask struct {

	// funcname
	fn map[string]func(string) string
}

func New() *RawJsonMask {
	return &RawJsonMask{
		fn: make(map[string](func(string) string)),
	}
}

// AddFunc adds masking function associated with name.
func (jm *RawJsonMask) AddFunc(name string, f func(string) string) {
	jm.fn[name] = f
}

// Returns fields
func (jm *RawJsonMask) Fields(str interface{}, tag string) Fields {
	return jm.fields(str, tag, "")
}

func (jm *RawJsonMask) fields(src interface{}, tag string, parentAttr string) Fields {

	var res Fields

	s := reflect.ValueOf(src)
	t := s.Type()

	if t.Kind() == reflect.Ptr {
		s = reflect.New(s.Type().Elem())
		t = s.Type()
		if t.Kind() == reflect.Ptr {
			s = s.Elem()
			t = s.Type()
		}
	}

	// if t.Kind() == reflect.Ptr {
	// 	s = s.Elem()
	// 	t = s.Type()
	// }

	if t.Kind() != reflect.Struct {
		panic("buildRules parameter str expected to be a struct")
	}

	for i := 0; i < s.NumField(); i++ {
		sf := s.Field(i)
		tf := t.Field(i)

		if tf.PkgPath != "" {
			// For Go version >= 1.17, use the StructField.IsExported function instead.
			continue
		}

		attr := tf.Tag.Get("json")

		if !(tf.Type.Name() == "" || tf.Anonymous) {
			if attr == "" || attr[0] == ',' {
				attr = tf.Name
			} else {
				// if struct attr tag has additional properties after name.
				idx := strings.IndexByte(attr, ',')
				if idx >= 0 {
					attr = attr[:idx]
				}
			}
		}

		a := Field{name: attr, tag: tf.Tag.Get(tag)}

		if tf.Type.Kind() == reflect.Slice {
			v := reflect.New(tf.Type.Elem()).Elem()
			if v.Type().Kind() == reflect.Struct {
				a.child = jm.fields(v.Interface(), tag, "")
				res = append(res, a)
				continue
			}
		}

		if parentAttr != "" {
			attr = parentAttr + "." + attr
			a.name = attr
		}

		if tf.Anonymous || tf.Type.Kind() == reflect.Struct {
			if tf.Type.Kind() != reflect.Ptr {
				res = append(res, jm.fields(sf.Interface(), tag, attr)...)
			} else {
				if sf.IsNil() && sf.Kind() == reflect.Ptr {
					sf = reflect.New(tf.Type.Elem())
					res = append(res, jm.fields(sf.Interface(), tag, attr)...)
				} else {
					// res = append(res, "")
				}
			}
			continue
		}
		res = append(res, a)
	}
	return res
}

func (jm *RawJsonMask) Mask(src []byte, fields Fields) ([]byte, error) {
	dst := make([]byte, len(src))
	copy(dst, src)
	return jm.mask(dst, "", fields, false)
}

func (rjm *RawJsonMask) mask(buf []byte, parentAttr string, r Fields, isSlice bool) ([]byte, error) {

	var err error

	for i := range r {
		attr := r[i].name
		tag := r[i].tag

		if isSlice {
			attr = parentAttr + ".#." + attr
		}

		if len(r[i].child) > 0 {
			buf, err = rjm.mask(buf, attr, r[i].child, true)
			if err != nil {
				return nil, err
			}
			continue
		}

		switch tag {
		case "":
			break
		case "-":
			if !isSlice {
				buf, err = sjson.DeleteBytes(buf, attr)
			} else {
				n := gjson.GetBytes(buf, parentAttr+".#")
				if !n.Exists() {
					break
				}

				for j := int64(0); j < n.Int(); j++ {
					key := parentAttr + "." + strconv.FormatInt(j, 10) + "." + r[i].name
					buf, err = sjson.DeleteBytes(buf, key)
					if err != nil {
						return nil, err
					}
				}
			}

		default:
			fn, ok := rjm.fn[tag]
			if !ok {
				break
			}
			v := gjson.GetBytes(buf, attr)
			mv := fn(v.String())
			buf, err = sjson.SetBytes(buf, attr, mv)
		}
		if err != nil {
			return nil, err
		}
	}

	return buf, nil
}
