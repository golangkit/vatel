package jsonmask

import (
	"fmt"
	"testing"
)

func TestJsonMasker_Mask(t *testing.T) {

	src := [][]byte{
		[]byte(`
{"id" : 0, 
"name" : "Robert", 
"mobileNumber" : "909090", 
"email" : "robert.egorov@gmail.com", 
"notice" : "this is long text",
"document" : "passport"}`),
		[]byte(`
{
	"id" : 1,
	"user" : {
		"firstName" : "Robert",
		"email" : "robert.egorov@gmail.com"
	}
}
`),
		[]byte(`
{
	"id" : 2,
	"emails" : [
		{
			"email" : "robert.egorov@gmail.com",
			"typeId" : 1,
			"isDefault" : false
		}, 
		{
			"email" : "robert.ego.rov@gmail.com",
			"typeId" : 2,
			"isDefault": true
		}
	]
}`),
		[]byte(`
{
	"id" : 3,
	"email" : "robert.egorov@gmail.com",
	"typeId" : 1,
	"isDefault" : false
}`),
		[]byte(`
{
	"id" : 4,
	"email" : {"email": "robert.egorov@gmail.com", "typeId": 3, "isDefault": true},
	"typeId" : 1,
	"isDefault" : false
}`),
		[]byte(`
{
	"id" : 5,
	"email" : {"email": "robert.egorov@gmail.com", "typeId": 3, "isDefault": true},
	"perms" : ["Hello", "World"s]
}`)}

	type Target struct {
		ID           int    `json:"id"`
		Name         string `json:"name" mask:"name"`
		MobileNumber string `json:"mobileNumber" mask:"mobile"`
		Email        string `json:"email" mask:"email"`
		Notice       string `json:"notice,omitempty" mask:"-"`
		Document     string `json:"document"`
		privateAttr  int    `mask:"hello"`
	}

	type Target2 struct {
		ID   int `json:"id"`
		User struct {
			FirstName string `json:"firstName" mask:"-"`
			Email     string `json:"email" mask:"email"`
		} `json:"user"`
	}
	type Email struct {
		Email     string `json:"email" mask:"email"`
		TypeID    string `json:"typeId" mask:"-"`
		IsDefault string `json:"isDefault"`
	}

	type Target3 struct {
		ID     int     `json:"id"`
		Emails []Email `json:"emails"`
	}

	type Target4 struct {
		ID int `json:"id"`
		*Email
	}

	type Target5 struct {
		ID    int   `json:"id"`
		Email Email `json:"email"`
	}

	var t5 *Target5
	_ = t5

	type Target6 struct {
		ID    int    `json:"id"`
		Email *Email `json:"email"`
	}

	type Target7 struct {
		ID    int      `json:"id"`
		Email *Email   `json:"email"`
		Perms []string `json:"perms" mask:"-"`
	}
	var t7 *Target7

	ts := []struct {
		src    []byte
		target interface{}
	}{{src[0], Target{}}, {src[1], Target2{}}, {src[2], Target3{}}, {src[3], Target4{}}, {src[4], Target5{}},
		{src[4], interface{}(t5)}, {src[4], Target6{}},
		{src[5], interface{}(t7)}}

	jm := New()
	jm.AddFunc("email", maskEmail)

	for i := range ts {
		fields := jm.Fields(ts[i].target, "mask")
		res, err := jm.Mask(ts[i].src, fields)
		if err != nil {
			t.Error(err)
		}
		fmt.Printf("result: %v\n", string(res))
		//fmt.Printf("result: %v\n", jm.buildStruct(ts[i].target, "mask"))
	}
}

func maskEmail(e string) string {
	return "***"
}
