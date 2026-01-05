package test

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

type type1 struct{}

type type2 struct{}

func TestEventsEmptyOk(t *testing.T) {
	test := &testing.T{}
	assert.True(t, AssertForefrontEvents(Context(t), test, make(chan any)))
	assert.False(t, test.Failed())
}

func TestEventsEmptyUnexpected(t *testing.T) {
	test := &testing.T{}
	assert.False(t, AssertForefrontEvents(Context(t), test, make(chan any), type1{}))
	assert.True(t, test.Failed())
}

func TestEventsOK(t *testing.T) {
	test := &testing.T{}
	events := make(chan any, 1)
	events <- type1{}
	assert.True(t, AssertForefrontEvents(Context(t), test, events, type1{}))
	assert.False(t, test.Failed())
}

func TestEventsNoExpected1(t *testing.T) {
	test := &testing.T{}
	events := make(chan any, 1)
	events <- type1{}
	assert.False(t, AssertForefrontEvents(Context(t), test, events, type2{}))
	assert.True(t, test.Failed())
}

func TestEventsNoExpected2(t *testing.T) {
	test := &testing.T{}
	events := make(chan any, 1)
	events <- type1{}
	assert.False(t, AssertForefrontEvents(Context(t), test, events, type1{}, type1{}))
	assert.True(t, test.Failed())
}

func TestEventsUnexpectedOK(t *testing.T) {
	test := &testing.T{}
	events := make(chan any, 2)
	events <- type1{}
	events <- type2{}
	assert.True(t, AssertForefrontEvents(Context(t), test, events, type1{}))
	assert.False(t, test.Failed())
}

func TestEventsUnexpectedFail(t *testing.T) {
	test := &testing.T{}
	events := make(chan any, 2)
	events <- type1{}
	events <- type2{}
	assert.False(t, AssertEvents(Context(t), test, events, type1{}))
	assert.True(t, test.Failed())
}

func TestEventsSequence(t *testing.T) {
	test := &testing.T{}
	events := make(chan any, 2)
	events <- type1{}
	events <- type2{}
	assert.True(t, AssertForefrontEvents(Context(t), test, events, type1{}, type2{}))
	assert.False(t, test.Failed())
}

func TestEventsPointer(t *testing.T) {
	test := &testing.T{}
	events := make(chan any, 1)
	events <- type1{}
	assert.False(t, AssertForefrontEvents(Context(t), test, events, &type1{}))
	assert.True(t, test.Failed())
}
