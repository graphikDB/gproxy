package codec

import (
	"github.com/mwitkow/grpc-proxy/proxy"
	"google.golang.org/grpc"
	"google.golang.org/grpc/encoding"
)

func NewProxyCodec() encoding.Codec {
	return &proxyCodec{codec: proxy.Codec()}
}

type proxyCodec struct {
	codec grpc.Codec
}

func (p *proxyCodec) Name() string {
	return "proto"
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
