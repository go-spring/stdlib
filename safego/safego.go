/*
 * Copyright 2024 The Go-Spring Authors.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *      https://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

// Package safego provides utilities for running goroutines safely with
// built-in panic recovery.
//
// Goroutines may panic due to programming errors such as nil pointer
// dereferences or out-of-bounds access. While such panics would normally
// crash the entire process, they can be recovered inside the goroutine.
//
// This package offers wrappers that launch goroutines with automatic panic
// recovery. When a panic is recovered, a global OnPanic handler is invoked.
// This allows applications to log panics, emit metrics, or trigger alerts,
// making failures in concurrent code easier to observe and debug.
package safego

import (
	"context"
	"fmt"
	"runtime/debug"
	"sync"

	"github.com/go-spring/stdlib/errutil"
)

// PanicInfo contains information about a recovered panic.
type PanicInfo struct {
	Panic any
	Stack []byte
}

// OnPanic is a global callback invoked whenever a panic is recovered inside
// a goroutine launched by this package.
//
// By default, it prints the panic value and stack trace to stdout.
// Applications may override this function during initialization to provide
// custom logging, metrics, or alerting behavior.
var OnPanic = func(ctx context.Context, info PanicInfo) {
	fmt.Printf("[PANIC] %v\n%s\n", info.Panic, info.Stack)
}

// Status represents the execution status of a goroutine and provides
// a mechanism to wait for its completion.
type Status struct {
	wg sync.WaitGroup
}

// newStatus creates and initializes a new Status instance.
func newStatus() *Status {
	s := &Status{}
	s.wg.Add(1)
	return s
}

// done marks the goroutine as completed.
func (s *Status) done() {
	s.wg.Done()
}

// Wait blocks until the associated goroutine completes.
func (s *Status) Wait() {
	s.wg.Wait()
}

// Go launches a goroutine that executes the provided function f with panic
// recovery enabled. If the goroutine panics, the panic is recovered and the
// global OnPanic handler is invoked.
//
// The provided context is passed to both f and OnPanic. The goroutine does
// not automatically stop when the context is canceled; the function f is
// responsible for observing ctx.Done() and returning when appropriate.
//
// If withoutCancel is true, the goroutine receives a context that is not
// canceled when the parent context is canceled.
func Go(ctx context.Context, f func(ctx context.Context), withoutCancel bool) *Status {
	if withoutCancel {
		ctx = context.WithoutCancel(ctx)
	}
	s := newStatus()
	go func() {
		defer s.done()
		defer func() {
			if r := recover(); r != nil {
				if OnPanic != nil {
					OnPanic(ctx, PanicInfo{r, debug.Stack()})
				}
			}
		}()
		f(ctx)
	}()
	return s
}

// ValueStatus represents the execution status of a goroutine that returns
// a value and an error.
type ValueStatus[T any] struct {
	wg  sync.WaitGroup
	val T
	err error
}

// newValueStatus creates and initializes a new ValueStatus instance.
func newValueStatus[T any]() *ValueStatus[T] {
	s := &ValueStatus[T]{}
	s.wg.Add(1)
	return s
}

// done marks the goroutine as completed.
func (s *ValueStatus[T]) done() {
	s.wg.Done()
}

// Wait blocks until the goroutine completes and returns its value and error.
func (s *ValueStatus[T]) Wait() (T, error) {
	s.wg.Wait()
	return s.val, s.err
}

type GoValueFunc[T any] func(ctx context.Context) (T, error)

// GoValue launches a goroutine that executes the provided function f,
// captures its returned value and error, and recovers from panics.
// If a panic occurs, it is recovered, reported via the global OnPanic
// handler, and converted into an error returned by Wait.
//
// The provided context is passed to both f and OnPanic. As with Go,
// cancellation is cooperative: f must observe ctx.Done() if early
// termination is desired.
//
// If withoutCancel is true, the goroutine receives a context that is
// not canceled when the parent context is canceled.
func GoValue[T any](ctx context.Context, f GoValueFunc[T], withoutCancel bool) *ValueStatus[T] {
	if withoutCancel {
		ctx = context.WithoutCancel(ctx)
	}
	s := newValueStatus[T]()
	go func() {
		defer s.done()
		defer func() {
			if r := recover(); r != nil {
				stack := debug.Stack()
				if OnPanic != nil {
					OnPanic(ctx, PanicInfo{r, stack})
				}
				s.err = errutil.Explain(nil, "panic recovered: %v\n%s", r, stack)
			}
		}()
		s.val, s.err = f(ctx)
	}()
	return s
}
