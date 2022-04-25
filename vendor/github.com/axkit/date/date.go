// The package date implements special type to replace standard time.Time
// in places where ony date part is required.
package date

import (
	"bytes"
	"database/sql/driver"
	"errors"
	"fmt"
	"reflect"
	"time"
)

// Date is an nullable date type without time and timezone.
// In memory it stores year, month and day like joined hex integer 0x20180131
// what makes it comparable and sortable as integer. In database it stores
// as DATE. Null value is represented in memory as 0.
type Date uint32

var (
	Separator         byte = '-'
	DatabaseSeparator byte = '-'
	tmpl                   = [10]byte{'0', '0', '0', '0', Separator, '0', '0', Separator, '0', '0'}
	tmplDB                 = [10]byte{'0', '0', '0', '0', DatabaseSeparator, '0', '0', DatabaseSeparator, '0', '0'}
	tmplJS                 = [12]byte{'"', '0', '0', '0', '0', Separator, '0', '0', Separator, '0', '0', '"'}

	// pfm stores preformatted dates before and after today,
	// used for convertation improvement.
	pfm map[Date]string

	// pjm stores preformatted json dates before and after today,
	// used for date unmarshal improvement.
	pjm map[string]Date

	null = []byte("null")

	cache string

	FiveYearBefore = Today().Add(-5, 0, 0)
	FiveYearAfter  = Today().Add(5, 0, 0)
)

// InitPreformattedValues
func InitPreformattedValues(from, to Date) {

	pfm = make(map[Date]string, (to.Time().Sub(from.Time()))/(24*time.Hour)+1)
	pjm = make(map[string]Date, (to.Time().Sub(from.Time()))/(24*time.Hour)+1)

	d := from
	for d < to {
		d = d.Add(0, 0, 1)
		pfm[d] = d.String()

		//var b [10]byte
		//d.byteArr(&b)
		pjm[d.String()] = d
	}
}

// Time converts Date to Time.
func (d Date) Time() time.Time {
	return time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, time.Local)
}

func (d Date) byteArr(res *[10]byte) {

	*res = tmpl
	i := uint32(d)
	zero := byte('0')

	// cycle could be used but straight way looks more effective.
	/*
		var b [8]byte
		val := uint32(d)
		for i := uint8(0); i < 8; i++ {
			b[7-i] = byte(uint8('0') + uint8(val&0x0000000F))
			val = val >> 4
		}
		return b

	*/
	res[0] = byte((i>>28)&0x0000000F) + zero
	res[1] = byte((i>>24)&0x0000000F) + zero
	res[2] = byte((i>>20)&0x0000000F) + zero
	res[3] = byte((i>>16)&0x0000000F) + zero

	res[5] = byte((i>>12)&0x0000000F) + zero
	res[6] = byte((i>>8)&0x0000000F) + zero

	res[8] = byte((i>>4)&0x0000000F) + zero
	res[9] = byte(i&0x0000000F) + zero

	return
}

func (d Date) byteSlice(res []byte) {

	i := uint32(d)
	zero := byte('0')

	res[0] = byte((i>>28)&0x0000000F) + zero
	res[1] = byte((i>>24)&0x0000000F) + zero
	res[2] = byte((i>>20)&0x0000000F) + zero
	res[3] = byte((i>>16)&0x0000000F) + zero

	res[5] = byte((i>>12)&0x0000000F) + zero
	res[6] = byte((i>>8)&0x0000000F) + zero

	res[8] = byte((i>>4)&0x0000000F) + zero
	res[9] = byte(i&0x0000000F) + zero

	return
}

// String implements interface Stringer. Returns empty string if date is empty.
// If not, returns date as a string YYYY-MM-DD.
func (d *Date) String() string {

	if s, ok := pfm[*d]; ok {
		return s
	}

	if !d.Valid() {
		return ""
	}

	return d.string()
}

func (d Date) string() string {
	var buf [10]byte
	d.byteArr(&buf)
	return string(buf[:])
}

// Add adds years, months or days or all together.
func (d Date) Add(years, months, days int) Date {
	t := time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, time.UTC)
	t = t.AddDate(years, months, days)
	return New(t.Date())
}

// Parse parses input string. String expected in format YYYY*MM*DD where
// * is any single char separator.
func (d *Date) Parse(s string) error {

	dt, err := Parse(s)
	if err != nil {
		return err
	}

	*d = dt
	return nil
}

// Year returns year of the date.
func (d Date) Year() int {
	i := uint32(d)
	return int((i>>28)*1000 + ((i>>24)&0x000F)*100 + ((i>>20)&0x000F)*10 + (i>>16)&0x000F)

}

// Month returns month of the date.
func (d Date) Month() time.Month {
	i := (uint32(d) >> 8) & 0x000000FF
	return time.Month((i>>4)*10 + i&0x0F)
}

// Day returns day of the date.
func (d Date) Day() int {
	i := uint32(d) & 0x000000FF
	return int((i>>4)*10 + i&0x0F)
}

// Parse decodes string YYYY-MM-DD or quoted "YYYY-MM-DD" to Date.
// TODO: improve performance by implementing date parsing without
// calling time.Parse().
func Parse(s string) (Date, error) {

	if s[0] == '"' {
		s = s[1:]
	}

	t, err := parseYYYYMMDD([]byte(s))
	if err != nil {
		return Null(), err
	}

	return newDate(t.Year(), t.Month(), t.Day()), nil
}

func parseYYYYMMDD(b []byte) (Date, error) {

	if len(b) < 10 {
		return Null(), errors.New("input date length less then 10 bytes")
	}

	var (
		y    int
		m, d int
	)

	y = int(b[0]-'0')*1000 + int(b[1]-'0')*100 + int(b[2]-'0')*10 + int(b[3]-'0')
	m = int(b[5]-'0')*10 + int(b[6]-'0')
	d = int(b[8]-'0')*10 + int(b[9]-'0')

	if y > 9999 || m > 12 || d > 31 {
		return Null(), errors.New("date parse errror")
	}

	// validate date
	t := time.Date(y, time.Month(m), d, 0, 0, 0, 0, time.Local)
	if t.Year() != y || t.Month() != time.Month(m) || t.Day() != d {
		return Null(), errors.New("date parse errror")
	}
	return newDate(y, time.Month(m), d), nil

}

func newDate(y int, m time.Month, d int) Date {
	return Date(dec2hexy(uint32(y))<<16 | (dec2hex(uint32(m)) << 8) | dec2hex(uint32(d)))
}

// dec2hex converts month or day 31 to 0x31
func dec2hex(i uint32) uint32 {
	return (i/10)*16 + i%10
}

// dec2hexy converts year 2018 to 0x2018
func dec2hexy(i uint32) uint32 {
	return (i/1000)*16*16*16 + ((i%1000)/100)*16*16 + (i%100)/10*16 + i%10
}

// Null returns null Date.
func Null() Date {
	return Date(0)
}

// Value implements interface sql.Valuer
func (d Date) Value() (driver.Value, error) {
	if !d.Valid() {
		return nil, nil
	}

	return []byte(d.String()), nil
}

// Valid return false if Date is null.
func (d Date) Valid() bool {
	return d > 0
}

// Scan implements database/sql Scanner interface.
func (d *Date) Scan(value interface{}) error {
	if value == nil {
		*d = Null()
		return nil
	}

	v, ok := value.(time.Time)
	if !ok {
		return fmt.Errorf("Date.Scan: expected date.Date, got %T (%q)", value, value)
	}

	*d = NewFromTime(v)
	return nil
}

// Today returns today's date.
func Today() Date {
	t := time.Now()
	return newDate(t.Year(), t.Month(), t.Day())
}

// New creates Date with specified year, month and day.
func New(y int, m time.Month, d int) Date {
	return newDate(y, m, d)
}

// Scan implements encoding/json Unmarshaller interface.
func (d *Date) UnmarshalJSON(b []byte) error {

	if (len(b) == 4 && bytes.Compare(b, null) == 0) || len(b) == 0 {
		*d = Null()
		return nil
	}

	b = b[1:11]
	v, ok := pjm[string(b)]
	if ok {
		*d = v
		return nil
	}

	var err error
	*d, err = parseYYYYMMDD(b)
	return err
}

// Scan implements encoding/json Unmarshaller interface.
func (d *Date) UnmarshalText(b []byte) error {
	return d.UnmarshalJSON(b)
}

// MarshalJSON implements encoding/json Marshaller interface.
func (d Date) MarshalJSON() ([]byte, error) {

	if !d.Valid() {
		return null, nil
	}

	res := tmplJS[:]
	d.byteSlice(res[1:])
	return res, nil
}

// NewFromTime creates Date from Time. Be careful with timezones.
func NewFromTime(t time.Time) Date {
	return newDate(t.Year(), t.Month(), t.Day())
}

func YearsBetweenToday(dt Date) int {
	return YearsBetween(dt, Today())
}

// YearsBetween количество лет между двумя датами.
func YearsBetween(past, now Date) int {
	age := now.Year() - past.Year()

	switch {
	case now.Month() > past.Month():
		return age
	case now.Month() < past.Month():
		return age - 1
	case now.Month() == past.Month():
		if now.Day() < past.Day() {
			return age - 1
		}
	}
	return age
}

func (d *Date) Format(s string) string {
	return d.Time().Format(s)
}

func (d Date) Date() (int, time.Month, int) {
	return d.Year(), d.Month(), d.Day()
}

func Converter(value string) reflect.Value {
	if v, err := Parse(value); err == nil {
		return reflect.ValueOf(v)
	}
	return reflect.Value{}
}
