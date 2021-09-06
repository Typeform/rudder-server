/*
 * Licensed to the Apache Software Foundation (ASF) under one
 * or more contributor license agreements. See the NOTICE file
 * distributed with this work for additional information
 * regarding copyright ownership. The ASF licenses this file
 * to you under the Apache License, Version 2.0 (the
 * "License"); you may not use this file except in compliance
 * with the License. You may obtain a copy of the License at
 *
 *   http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing,
 * software distributed under the License is distributed on an
 * "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
 * KIND, either express or implied. See the License for the
 * specific language governing permissions and limitations
 * under the License.
 */

package thrift

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"
)

const DEFAULT_MAX_LENGTH = 16384000

type TFramedTransport struct {
	transport TTransport
	maxLength uint32

	writeBuf bytes.Buffer

	reader  *bufio.Reader
	readBuf bytes.Buffer

	buffer [4]byte
}

type tFramedTransportFactory struct {
	factory   TTransportFactory
	maxLength uint32
}

func NewTFramedTransportFactory(factory TTransportFactory) TTransportFactory {
	return &tFramedTransportFactory{factory: factory, maxLength: DEFAULT_MAX_LENGTH}
}

func NewTFramedTransportFactoryMaxLength(factory TTransportFactory, maxLength uint32) TTransportFactory {
	return &tFramedTransportFactory{factory: factory, maxLength: maxLength}
}

func (p *tFramedTransportFactory) GetTransport(base TTransport) (TTransport, error) {
	tt, err := p.factory.GetTransport(base)
	if err != nil {
		return nil, err
	}
	return NewTFramedTransportMaxLength(tt, p.maxLength), nil
}

func NewTFramedTransport(transport TTransport) *TFramedTransport {
	return &TFramedTransport{transport: transport, reader: bufio.NewReader(transport), maxLength: DEFAULT_MAX_LENGTH}
}

func NewTFramedTransportMaxLength(transport TTransport, maxLength uint32) *TFramedTransport {
	return &TFramedTransport{transport: transport, reader: bufio.NewReader(transport), maxLength: maxLength}
}

func (p *TFramedTransport) Open() error {
	return p.transport.Open()
}

func (p *TFramedTransport) IsOpen() bool {
	return p.transport.IsOpen()
}

func (p *TFramedTransport) Close() error {
	return p.transport.Close()
}

func (p *TFramedTransport) Read(buf []byte) (read int, err error) {
	read, err = p.readBuf.Read(buf)
	if err != io.EOF {
		return
	}

	// For bytes.Buffer.Read, EOF would only happen when read is zero,
	// but still, do a sanity check,
	// in case that behavior is changed in a future version of go stdlib.
	// When that happens, just return nil error,
	// and let the caller call Read again to read the next frame.
	if read > 0 {
		return read, nil
	}

	// Reaching here means that the last Read finished the last frame,
	// so we need to read the next frame into readBuf now.
	if err = p.readFrame(); err != nil {
		return read, err
	}
	newRead, err := p.Read(buf[read:])
	return read + newRead, err
}

func (p *TFramedTransport) ReadByte() (c byte, err error) {
	buf := p.buffer[:1]
	_, err = p.Read(buf)
	if err != nil {
		return
	}
	c = buf[0]
	return
}

func (p *TFramedTransport) Write(buf []byte) (int, error) {
	n, err := p.writeBuf.Write(buf)
	return n, NewTTransportExceptionFromError(err)
}

func (p *TFramedTransport) WriteByte(c byte) error {
	return p.writeBuf.WriteByte(c)
}

func (p *TFramedTransport) WriteString(s string) (n int, err error) {
	return p.writeBuf.WriteString(s)
}

func (p *TFramedTransport) Flush(ctx context.Context) error {
	size := p.writeBuf.Len()
	buf := p.buffer[:4]
	binary.BigEndian.PutUint32(buf, uint32(size))
	_, err := p.transport.Write(buf)
	if err != nil {
		p.writeBuf.Reset()
		return NewTTransportExceptionFromError(err)
	}
	if size > 0 {
		if _, err := io.Copy(p.transport, &p.writeBuf); err != nil {
			p.writeBuf.Reset()
			return NewTTransportExceptionFromError(err)
		}
	}
	err = p.transport.Flush(ctx)
	return NewTTransportExceptionFromError(err)
}

func (p *TFramedTransport) readFrame() error {
	buf := p.buffer[:4]
	if _, err := io.ReadFull(p.reader, buf); err != nil {
		return err
	}
	size := binary.BigEndian.Uint32(buf)
	if size < 0 || size > p.maxLength {
		return NewTTransportException(UNKNOWN_TRANSPORT_EXCEPTION, fmt.Sprintf("Incorrect frame size (%d)", size))
	}
	_, err := io.CopyN(&p.readBuf, p.reader, int64(size))
	return NewTTransportExceptionFromError(err)
}

func (p *TFramedTransport) RemainingBytes() (num_bytes uint64) {
	return uint64(p.readBuf.Len())
}
