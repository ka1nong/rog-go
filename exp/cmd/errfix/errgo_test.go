package main

func init() {
	addTestCases(errgoMaskTests, errgoMask)
	addTestCases(errgoCauseTests, errgoCause)
}

var errgoMaskTests = []testCase{{
	Name: "errgo-mask.0",
	In: `package main

import (
	"errors"
	"fmt"
	"github.com/errgo/errgo"
	gc "launchpad.net/gocheck"
)

var errSomething = errors.New("foo")

func f() error {
	if err := foo(); err != nil {
		return fmt.Errorf("failure: %v", err)
	}
	errgo.New("foo: %s, %s", arg1, arg2)
	errgo.Annotate(err, "blah")
	errgo.Annotatef(err, "blah: %s, %s", arg1, arg2)
	return fmt.Errorf("cannot something: %s, %s", x, y)
}

func wrapper() (int, error) {
	if x, err := foo(); err != nil {
		return 0, err
	}
	if err := foo(); err != nil {
		return 0, err // A comment
	}
	return 24, nil
}
`,
	Out: `package main

import (
	"fmt"
	"launchpad.net/errgo/v2/errors"
	gc "launchpad.net/gocheck"
)

var errSomething = errors.New("foo")

func f() error {
	if err := foo(); err != nil {
		return errors.Notef(err, "failure")
	}
	errors.Newf("foo: %s, %s", arg1, arg2)
	errors.NoteMask(err, "blah")
	errors.Notef(err, "blah: %s, %s", arg1, arg2)
	return errors.Newf("cannot something: %s, %s", x, y)
}

func wrapper() (int, error) {
	if x, err := foo(); err != nil {
		return 0, errors.Mask(err)
	}
	if err := foo(); err != nil {
		return 0, errors. // A comment
					Mask(err)
	}
	return 24, nil
}
`,
}}

var errgoCauseTests = []testCase{{
	Name: "errgo-cause.0",
	In: `package main

import (
	"errors"
	"fmt"
	gc "launchpad.net/gocheck"
)

func (*suite) TestSomething(c *gc.C) {
	err := foo()
	c.Check(err, gc.Equals, errSomething)
	c.Check(err, gc.Not(gc.Equals), errSomething)
	c.Check(err, gc.Equals, nil)
	c.Check(err, gc.Not(gc.Equals), nil)
}

func tester() error {
	if err := foo(); err == errSomething {
		return nil
	}
	if err := foo(); err == nil {
		return nil
	}
	return nil
}
`,
	Out: `package main

import (
	"fmt"
	"launchpad.net/errgo/v2/errors"
	gc "launchpad.net/gocheck"
)

func (*suite) TestSomething(c *gc.C) {
	err := foo()
	c.Check(errors.Cause(err), gc.Equals, errSomething)
	c.Check(errors.Cause(err), gc.Not(gc.Equals), errSomething)
	c.Check(err, gc.Equals, nil)
	c.Check(err, gc.Not(gc.Equals), nil)
}

func tester() error {
	if err := foo(); errors.Cause(err) == errSomething {
		return nil
	}
	if err := foo(); err == nil {
		return nil
	}
	return nil
}
`,
}}