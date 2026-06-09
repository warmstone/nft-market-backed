package graphql

import (
	"fmt"
	"io"
	"math/big"
	"strconv"

	"github.com/99designs/gqlgen/graphql"
)

// BigInt is a type alias for *big.Int used as the Go model for the BigInt scalar.
type BigInt = *big.Int

func MarshalBigInt(bi *big.Int) graphql.Marshaler {
	return graphql.WriterFunc(func(w io.Writer) {
		if bi == nil {
			_, _ = io.WriteString(w, "null")
			return
		}
		_, _ = io.WriteString(w, strconv.Quote(bi.String()))
	})
}

func UnmarshalBigInt(v interface{}) (*big.Int, error) {
	switch v := v.(type) {
	case string:
		i := new(big.Int)
		if _, ok := i.SetString(v, 10); !ok {
			return nil, fmt.Errorf("invalid BigInt: %s", v)
		}
		return i, nil
	case nil:
		return nil, nil
	default:
		return nil, fmt.Errorf("BigInt must be a string, got %T", v)
	}
}
