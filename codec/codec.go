package codec

import (
	"google.golang.org/grpc"
	"google.golang.org/grpc/encoding"
)

func NewGrpcCodec() encoding.Codec {
	return &proxyCodec{}
}

type proxyCodec struct {
	codec grpc.Codec
}

func (p *proxyCodec) Name() string {
	return "proxy"
}

func (p *proxyCodec) Marshal(v interface{}) ([]byte, error) {
	return p.codec.Marshal(v)
}

func (p *proxyCodec) Unmarshal(data []byte, v interface{}) error {
	return p.codec.Unmarshal(data, v)
}

func (p *proxyCodec) String() string {
	return p.codec.String()
}
