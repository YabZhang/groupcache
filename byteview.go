/*
Copyright 2012 Google Inc.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

     http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package groupcache

import (
	"bytes"
	"errors"
	"io"
	"strings"
)

// A ByteView holds an immutable view of bytes.
// Internally it wraps either a []byte or a string,
// but that detail is invisible to callers.
//
// A ByteView is meant to be used as a value type, not
// a pointer (like a time.Time).
// ByteView 是一个只读的数据结构，用来表示缓存值。
// 它内部可以包装 []byte 或者 string。
// 选择它的目的是为了性能和内存安全：
// 1. 它是不可变的 (immutable)，这意味着可以放心地在并发环境（缓存系统本身就是高并发的）
//    下被多个 goroutine 共享，而不需要额外的读写锁来保护底层数据，也防止了外部使用者不小心修改缓存。
// 2. 它设计为通过值传递（Value Type）而不是指针传递。
type ByteView struct {
	// If b is non-nil, b is used, else s is used.
	// b 存储真实的字节数组。
	// s 存储真实的字符串。
	// 为什么要有两个字段？为了避免在不需要的时候在 string 和 []byte 之间进行昂贵的强制转换（涉及内存拷贝）。
	// 在 Go 语言中，string 底层是只读的 []byte，直接互相转换会触发内存复制。
	b []byte
	s string
}

// Len returns the view's length.
func (v ByteView) Len() int {
	if v.b != nil {
		return len(v.b)
	}
	return len(v.s)
}

// ByteSlice returns a copy of the data as a byte slice.
// 返回底层的字节数组。
// 注意：如果底层是 b，它一定会返回一份 **拷贝**。
// 这是为了维护 ByteView 的不可变性。如果直接返回 v.b，外部调用者可能会修改里面的内容，
// 从而破坏缓存的一致性。
func (v ByteView) ByteSlice() []byte {
	if v.b != nil {
		return cloneBytes(v.b)
	}
	// 将 string 转换为 []byte 时，Go 编译器也会自动进行一次内存分配和拷贝。
	return []byte(v.s)
}

// String returns the data as a string, making a copy if necessary.
// 返回底层的字符串。
// 与 ByteSlice 不同，这里不需要显式拷贝（除非底层是 []byte），因为 Go 的 string 本身就是不可变的。
func (v ByteView) String() string {
	if v.b != nil {
		return string(v.b)
	}
	return v.s
}

// At returns the byte at index i.
func (v ByteView) At(i int) byte {
	if v.b != nil {
		return v.b[i]
	}
	return v.s[i]
}

// Slice slices the view between the provided from and to indices.
func (v ByteView) Slice(from, to int) ByteView {
	if v.b != nil {
		return ByteView{b: v.b[from:to]}
	}
	return ByteView{s: v.s[from:to]}
}

// SliceFrom slices the view from the provided index until the end.
func (v ByteView) SliceFrom(from int) ByteView {
	if v.b != nil {
		return ByteView{b: v.b[from:]}
	}
	return ByteView{s: v.s[from:]}
}

// Copy copies b into dest and returns the number of bytes copied.
func (v ByteView) Copy(dest []byte) int {
	if v.b != nil {
		return copy(dest, v.b)
	}
	return copy(dest, v.s)
}

// Equal returns whether the bytes in b are the same as the bytes in
// b2.
func (v ByteView) Equal(b2 ByteView) bool {
	if b2.b == nil {
		return v.EqualString(b2.s)
	}
	return v.EqualBytes(b2.b)
}

// EqualString returns whether the bytes in b are the same as the bytes
// in s.
func (v ByteView) EqualString(s string) bool {
	if v.b == nil {
		return v.s == s
	}
	l := v.Len()
	if len(s) != l {
		return false
	}
	for i, bi := range v.b {
		if bi != s[i] {
			return false
		}
	}
	return true
}

// EqualBytes returns whether the bytes in b are the same as the bytes
// in b2.
func (v ByteView) EqualBytes(b2 []byte) bool {
	if v.b != nil {
		return bytes.Equal(v.b, b2)
	}
	l := v.Len()
	if len(b2) != l {
		return false
	}
	for i, bi := range b2 {
		if bi != v.s[i] {
			return false
		}
	}
	return true
}

// Reader returns an io.ReadSeeker for the bytes in v.
func (v ByteView) Reader() io.ReadSeeker {
	if v.b != nil {
		return bytes.NewReader(v.b)
	}
	return strings.NewReader(v.s)
}

// ReadAt implements io.ReaderAt on the bytes in v.
func (v ByteView) ReadAt(p []byte, off int64) (n int, err error) {
	if off < 0 {
		return 0, errors.New("view: invalid offset")
	}
	if off >= int64(v.Len()) {
		return 0, io.EOF
	}
	n = v.SliceFrom(int(off)).Copy(p)
	if n < len(p) {
		err = io.EOF
	}
	return
}

// WriteTo implements io.WriterTo on the bytes in v.
func (v ByteView) WriteTo(w io.Writer) (n int64, err error) {
	var m int
	if v.b != nil {
		m, err = w.Write(v.b)
	} else {
		m, err = io.WriteString(w, v.s)
	}
	if err == nil && m < v.Len() {
		err = io.ErrShortWrite
	}
	n = int64(m)
	return
}
