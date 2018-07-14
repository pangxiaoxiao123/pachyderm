package require

import (
	"fmt"
	"reflect"
	"regexp"
	"runtime/debug"
	"testing"
	"time"
)

// Matches checks that a string matches a regular-expression.
func Matches(tb testing.TB, expectedMatch string, actual string, msgAndArgs ...interface{}) {
	tb.Helper()
	r, err := regexp.Compile(expectedMatch)
	if err != nil {
		fatal(tb, msgAndArgs, "Match string provided (%v) is invalid", expectedMatch)
	}
	if !r.MatchString(actual) {
		fatal(tb, msgAndArgs, "Actual string (%v) does not match pattern (%v)", actual, expectedMatch)
	}
}

// OneOfMatches checks whether one element of a slice matches a regular-expression.
func OneOfMatches(tb testing.TB, expectedMatch string, actuals []string, msgAndArgs ...interface{}) {
	tb.Helper()
	r, err := regexp.Compile(expectedMatch)
	if err != nil {
		fatal(tb, msgAndArgs, "Match string provided (%v) is invalid", expectedMatch)
	}
	for _, actual := range actuals {
		if r.MatchString(actual) {
			return
		}
	}
	fatal(tb, msgAndArgs, "None of actual strings (%v) match pattern (%v)", actuals, expectedMatch)

}

// Equal checks equality of two values.
func Equal(tb testing.TB, expected interface{}, actual interface{}, msgAndArgs ...interface{}) {
	tb.Helper()
	eV, aV := reflect.ValueOf(expected), reflect.ValueOf(actual)
	if eV.Type() != aV.Type() {
		fatal(
			tb,
			msgAndArgs,
			"Not equal: %T(%#v) (expected)\n"+
				"        != %T(%#v) (actual)", expected, expected, actual, actual)
	}
	if !reflect.DeepEqual(expected, actual) {
		fatal(
			tb,
			msgAndArgs,
			"Not equal: %#v (expected)\n"+
				"        != %#v (actual)", expected, actual)
	}
}

// NotEqual checks inequality of two values.
func NotEqual(tb testing.TB, expected interface{}, actual interface{}, msgAndArgs ...interface{}) {
	tb.Helper()
	if reflect.DeepEqual(expected, actual) {
		fatal(
			tb,
			msgAndArgs,
			"Equal: %#v (expected)\n"+
				"    == %#v (actual)", expected, actual)
	}
}

// ElementsEqualOrErr returns nil if the elements of the slice "expecteds" are
// exactly the elements of the slice "actuals", ignoring order (i.e.
// setwise-equal), and an error otherwise.
//
// Unlike other require.* functions, this returns an error, so that if the
// caller is polling e.g. ListCommit or ListAdmins, they can wrap
// ElementsEqualOrErr in a retry loop.
//
// Also, like ElementsEqual, treat 'nil' and the empty slice as equivalent (for
// convenience)
func ElementsEqualOrErr(expecteds interface{}, actuals interface{}) error {
	es := reflect.ValueOf(expecteds)
	as := reflect.ValueOf(actuals)

	// If either slice is empty, check that both are empty
	esIsEmpty := expecteds == nil || es.IsNil() || (es.Kind() == reflect.Slice && es.Len() == 0)
	asIsEmpty := actuals == nil || as.IsNil() || (as.Kind() == reflect.Slice && as.Len() == 0)
	if esIsEmpty && asIsEmpty {
		return nil
	} else if esIsEmpty {
		return fmt.Errorf("expected 0 elements, but got %d: %v", as.Len(), actuals)
	} else if asIsEmpty {
		return fmt.Errorf("expected %d elements, but got 0\n  expected: %v", es.Len(), expecteds)
	}

	// Both slices are nonempty--compare elements
	if es.Kind() != reflect.Slice {
		return fmt.Errorf("\"expecteds\" must be a slice, but was %s", es.Type().String())
	}
	if as.Kind() != reflect.Slice {
		return fmt.Errorf("\"actuals\" must be a slice, but was %s", as.Type().String())
	}

	// Make sure expecteds and actuals are slices of the same type, modulo
	// pointers (*T ~= T in this function)
	esArePtrs := es.Type().Elem().Kind() == reflect.Ptr
	asArePtrs := as.Type().Elem().Kind() == reflect.Ptr
	esElemType, asElemType := es.Type().Elem(), as.Type().Elem()
	if esArePtrs {
		esElemType = es.Type().Elem().Elem()
	}
	if asArePtrs {
		asElemType = as.Type().Elem().Elem()
	}
	if esElemType != asElemType {
		return fmt.Errorf("Expected []%s but got []%s", es.Type().Elem(), as.Type().Elem())
	}

	// Count up elements of expecteds
	intType := reflect.TypeOf(int64(0))
	expectedCt := reflect.MakeMap(reflect.MapOf(esElemType, intType))
	for i := 0; i < es.Len(); i++ {
		v := es.Index(i)
		if esArePtrs {
			v = v.Elem()
		}
		if !expectedCt.MapIndex(v).IsValid() {
			expectedCt.SetMapIndex(v, reflect.ValueOf(int64(1)))
		} else {
			newCt := expectedCt.MapIndex(v).Int() + 1
			expectedCt.SetMapIndex(v, reflect.ValueOf(newCt))
		}
	}

	// Count up elements of actuals
	actualCt := reflect.MakeMap(reflect.MapOf(asElemType, intType))
	for i := 0; i < as.Len(); i++ {
		v := as.Index(i)
		if asArePtrs {
			v = v.Elem()
		}
		if !actualCt.MapIndex(v).IsValid() {
			actualCt.SetMapIndex(v, reflect.ValueOf(int64(1)))
		} else {
			newCt := actualCt.MapIndex(v).Int() + 1
			actualCt.SetMapIndex(v, reflect.ValueOf(newCt))
		}
	}
	if expectedCt.Len() != actualCt.Len() {
		// slight abuse of error: contains newlines so final output prints well
		return fmt.Errorf("expected %d distinct elements, but got %d\n  expected: %v\n  actual: %v", expectedCt.Len(), actualCt.Len(), expecteds, actuals)
	}
	for _, key := range expectedCt.MapKeys() {
		ec := expectedCt.MapIndex(key)
		ac := actualCt.MapIndex(key)
		if !ec.IsValid() || !ac.IsValid() || ec.Int() != ac.Int() {
			ecInt, acInt := int64(0), int64(0)
			if ec.IsValid() {
				ecInt = ec.Int()
			}
			if ac.IsValid() {
				acInt = ac.Int()
			}
			// slight abuse of error: contains newlines so final output prints well
			return fmt.Errorf("expected %d copies of %v, but got %d copies\n  expected: %v\n  actual: %v", ecInt, key, acInt, expecteds, actuals)
		}
	}
	return nil
}

// ElementsEqualUnderFn checks that the elements of the slice 'expecteds' are
// exactly the images of every element of the slice 'actuals' under 'f',
// ignoring order (i.e.  'expecteds' and 'map(f, actuals)' are setwise-equal,
// but respecting duplicates). This is useful for cases where ElementsEqual
// doesn't quite work, e.g. because the type in 'expecteds'/'actuals' contains a
// pointer, or 'actuals' contains superfluous data which you wish to discard
//
// Like ElementsEqual, treat 'nil' and the empty slice as equivalent (for
// convenience)
func ElementsEqualUnderFn(tb testing.TB, expecteds interface{}, actuals interface{}, f func(interface{}) interface{}, msgAndArgs ...interface{}) {
	tb.Helper()
	as := reflect.ValueOf(actuals)
	es := reflect.ValueOf(expecteds)

	// Check if 'actuals' is empty; if so, just pass nil (no need to transform)
	if actuals != nil && !as.IsNil() && as.Kind() != reflect.Slice {
		fatal(tb, msgAndArgs, fmt.Sprintf("\"actuals\" must be a slice, but was %s", as.Type().String()))
	} else if actuals == nil || as.IsNil() || as.Len() == 0 {
		// Just pass 'nil' for 'actuals'
		if err := ElementsEqualOrErr(expecteds, nil); err != nil {
			fatal(tb, msgAndArgs, err.Error())
		}
		return
	}

	// Check if 'expecteds' is empty: if so, return an error (since 'actuals' is
	// not empty)
	if expecteds != nil && !es.IsNil() && es.Kind() != reflect.Slice {
		fatal(tb, msgAndArgs, fmt.Sprintf("\"expecteds\" must be a slice, but was %s", as.Type().String()))
	} else if expecteds == nil || es.IsNil() || es.Len() == 0 {
		fatal(tb, msgAndArgs, fmt.Sprintf("expected 0 distinct elements, but got %d\n elements (before function is applied): %v", as.Len(), actuals))
	}

	// Neither 'expecteds' nor 'actuals' is empty--apply 'f' to 'actuals'
	newActuals := reflect.MakeSlice(reflect.SliceOf(es.Type().Elem()), as.Len(), as.Len())
	for i := 0; i < as.Len(); i++ {
		newActuals.Index(i).Set(reflect.ValueOf(f(as.Index(i).Interface())))
	}
	if err := ElementsEqualOrErr(expecteds, newActuals.Interface()); err != nil {
		fatal(tb, msgAndArgs, err.Error())
	}
}

// ElementsEqual checks that the elements of the slice "expecteds" are
// exactly the elements of the slice "actuals", ignoring order (i.e.
// setwise-equal, but respecting duplicates).
//
// Note that if the elements of 'expecteds' and 'actuals' are pointers,
// ElementsEqual will unwrap the pointers before comparing them, so that the
// output of e.g. ListCommit(), which returns []*pfs.Commit can easily be
// verfied.
//
// Also, treat 'nil' and the empty slice as equivalent, so that callers can
// pass 'nil' for 'expecteds'.
func ElementsEqual(tb testing.TB, expecteds interface{}, actuals interface{}, msgAndArgs ...interface{}) {
	tb.Helper()
	if err := ElementsEqualOrErr(expecteds, actuals); err != nil {
		fatal(tb, msgAndArgs, err.Error())
	}
}

// oneOfEquals is a helper function for EqualOneOf, OneOfEquals and NoneEquals, that simply
// returns a bool indicating whether 'elem' is in 'slice'. 'sliceName' is used for errors
func oneOfEquals(sliceName string, slice interface{}, elem interface{}) (bool, error) {
	e := reflect.ValueOf(elem)
	sl := reflect.ValueOf(slice)
	if slice == nil || sl.IsNil() {
		sl = reflect.MakeSlice(reflect.SliceOf(e.Type()), 0, 0)
	}
	if sl.Kind() != reflect.Slice {
		return false, fmt.Errorf("\"%s\" must a be a slice, but instead was %s", sliceName, sl.Type().String())
	}
	if e.Type() != sl.Type().Elem() {
		return false, nil
	}
	arePtrs := e.Kind() == reflect.Ptr
	for i := 0; i < sl.Len(); i++ {
		if !arePtrs && reflect.DeepEqual(e.Interface(), sl.Index(i).Interface()) {
			return true, nil
		} else if arePtrs && reflect.DeepEqual(e.Elem().Interface(), sl.Index(i).Elem().Interface()) {
			return true, nil
		}
	}
	return false, nil
}

// EqualOneOf checks if a value is equal to one of the elements of a slice. Note
// that if expecteds and actual are a slice of pointers and a pointer
// respectively, then the pointers are unwrapped before comparison (so this
// functions works for e.g. *pfs.Commit and []*pfs.Commit)
func EqualOneOf(tb testing.TB, expecteds interface{}, actual interface{}, msgAndArgs ...interface{}) {
	tb.Helper()
	equal, err := oneOfEquals("expecteds", expecteds, actual)
	if err != nil {
		fatal(tb, msgAndArgs, err.Error())
	}
	if !equal {
		fatal(
			tb,
			msgAndArgs,
			"None of : %#v (expecteds)\n"+
				"              == %#v (actual)", expecteds, actual)
	}
}

// OneOfEquals checks whether one element of a slice equals a value. Like
// EqualsOneOf, OneOfEquals unwraps pointers
func OneOfEquals(tb testing.TB, expected interface{}, actuals interface{}, msgAndArgs ...interface{}) {
	tb.Helper()
	equal, err := oneOfEquals("actuals", actuals, expected)
	if err != nil {
		fatal(tb, msgAndArgs, err.Error())
	}
	if !equal {
		fatal(tb, msgAndArgs,
			"Not equal : %#v (expected)\n"+
				" one of  != %#v (actuals)", expected, actuals)
	}
}

// NoneEquals checks one element of a slice equals a value. Like
// EqualsOneOf, NoneEquals unwraps pointers.
func NoneEquals(tb testing.TB, expected interface{}, actuals interface{}, msgAndArgs ...interface{}) {
	tb.Helper()
	equal, err := oneOfEquals("actuals", actuals, expected)
	if err != nil {
		fatal(tb, msgAndArgs, err.Error())
	}
	if equal {
		fatal(tb, msgAndArgs,
			"Equal : %#v (expected)\n == one of %#v (actuals)", expected, actuals)
	}
}

// NoError checks for no error.
func NoError(tb testing.TB, err error, msgAndArgs ...interface{}) {
	tb.Helper()
	if err != nil {
		fatal(tb, msgAndArgs, "No error is expected but got %s", err.Error())
	}
}

// NoErrorWithinT checks that 'f' finishes within time 't' and does not emit an
// error
func NoErrorWithinT(tb testing.TB, t time.Duration, f func() error, msgAndArgs ...interface{}) {
	tb.Helper()
	errCh := make(chan error)
	go func() {
		// This goro will leak if the timeout is exceeded, but it's okay because the
		// test is failing anyway
		errCh <- f()
	}()
	select {
	case err := <-errCh:
		if err != nil {
			fatal(tb, msgAndArgs, "No error is expected but got %s", err.Error())
		}
	case <-time.After(t):
		fatal(tb, msgAndArgs, "operation did not finish within %s", t.String())
	}
}

// NoErrorWithinTRetry checks that 'f' finishes within time 't' and does not
// emit an error. Unlike NoErrorWithinT if f does error, it will retry it.
func NoErrorWithinTRetry(tb testing.TB, t time.Duration, f func() error, msgAndArgs ...interface{}) {
	tb.Helper()
	doneCh := make(chan struct{})
	timeout := false
	go func() {
		for !timeout {
			if err := f(); err == nil {
				close(doneCh)
				break
			}
		}
	}()
	select {
	case <-doneCh:
	case <-time.After(t):
		timeout = true
		fatal(tb, msgAndArgs, "operation did not finish within %s", t.String())
	}
}

// YesError checks for an error.
func YesError(tb testing.TB, err error, msgAndArgs ...interface{}) {
	tb.Helper()
	if err == nil {
		fatal(tb, msgAndArgs, "Error is expected but got %v", err)
	}
}

// NotNil checks a value is non-nil.
func NotNil(tb testing.TB, object interface{}, msgAndArgs ...interface{}) {
	tb.Helper()
	success := true

	if object == nil {
		success = false
	} else {
		value := reflect.ValueOf(object)
		kind := value.Kind()
		if kind >= reflect.Chan && kind <= reflect.Slice && value.IsNil() {
			success = false
		}
	}

	if !success {
		fatal(tb, msgAndArgs, "Expected value not to be nil.")
	}
}

// Nil checks a value is nil.
func Nil(tb testing.TB, object interface{}, msgAndArgs ...interface{}) {
	tb.Helper()
	if object == nil {
		return
	}
	value := reflect.ValueOf(object)
	kind := value.Kind()
	if kind >= reflect.Chan && kind <= reflect.Slice && value.IsNil() {
		return
	}

	fatal(tb, msgAndArgs, "Expected value to be nil, but was %v", object)
}

// True checks a value is true.
func True(tb testing.TB, value bool, msgAndArgs ...interface{}) {
	tb.Helper()
	if !value {
		fatal(tb, msgAndArgs, "Should be true.")
	}
}

// False checks a value is false.
func False(tb testing.TB, value bool, msgAndArgs ...interface{}) {
	tb.Helper()
	if value {
		fatal(tb, msgAndArgs, "Should be false.")
	}
}

func logMessage(tb testing.TB, msgAndArgs []interface{}) {
	tb.Helper()
	if len(msgAndArgs) == 1 {
		tb.Logf(msgAndArgs[0].(string))
	}
	if len(msgAndArgs) > 1 {
		tb.Logf(msgAndArgs[0].(string), msgAndArgs[1:]...)
	}
}

func fatal(tb testing.TB, userMsgAndArgs []interface{}, msgFmt string, msgArgs ...interface{}) {
	tb.Helper()
	logMessage(tb, userMsgAndArgs)
	tb.Logf(msgFmt, msgArgs...)
	tb.Fatalf("current stack:\n%s", string(debug.Stack()))
}
