package pool

import (
	"github.com/mediocregopher/radix.v2/redis"
)

// Pool is a simple connection pool for redis Clients. It will create a small
// pool of initial connections, and if more connections are needed they will be
// created on demand. If a connection is Put back and the pool is full it will
// be closed.
type Pool struct {
	pool chan *redis.Client
	df   DialFunc

	// The network/address that the pool is connecting to. These are going to be
	// whatever was passed into the NewPool function. These should not be
	// changed after the pool is initialized
	Network, Addr string
}

// DialFunc is a function which can be passed into NewCustomPool
type DialFunc func(network, addr string) (*redis.Client, error)

// NewCustomPool is like NewPool except you can specify a DialFunc which will be
// used when creating new connections for the pool. The common use-case is to do
// authentication for new connections.
func NewCustomPool(network, addr string, size int, df DialFunc) (*Pool, error) {
	var client *redis.Client
	var err error
	pool := make([]*redis.Client, 0, size)
	for i := 0; i < size; i++ {
		client, err = df(network, addr)
		if err != nil {
			for _, client = range pool {
				client.Close()
			}
			pool = pool[0:]
			break
		}
		pool = append(pool, client)
	}
	p := Pool{
		Network: network,
		Addr:    addr,
		pool:    make(chan *redis.Client, len(pool)),
		df:      df,
	}
	for i := range pool {
		p.pool <- pool[i]
	}
	return &p, err
}

// NewPool creates a new Pool whose connections are all created using
// redis.Dial(network, addr). The size indicates the maximum number of idle
// connections to have waiting to be used at any given moment. If an error is
// encountered an empty (but still usable) pool is returned alongside that error
func NewPool(network, addr string, size int) (*Pool, error) {
	return NewCustomPool(network, addr, size, redis.Dial)
}

// Get retrieves an available redis client. If there are none available it will
// create a new one on the fly
func (p *Pool) Get() (*redis.Client, error) {
	select {
	case conn := <-p.pool:
		return conn, nil
	default:
		return p.df(p.Network, p.Addr)
	}
}

// Put returns a client back to the pool. If the pool is full the client is
// closed instead. If the client is already closed (due to connection failure or
// what-have-you) it will not be put back in the pool
func (p *Pool) Put(conn *redis.Client) {
	if conn.LastCritical == nil {
		select {
		case p.pool <- conn:
		default:
			conn.Close()
		}
	}
}

// Empty removes and calls Close() on all the connections currently in the pool.
// Assuming there are no other connections waiting to be Put back this method
// effectively closes and cleans up the pool.
func (p *Pool) Empty() {
	var conn *redis.Client
	for {
		select {
		case conn = <-p.pool:
			conn.Close()
		default:
			return
		}
	}
}

// With is a nice wrapper around Get and Put which takes in a function which
// will use a client connection (perhaps more than once) and handles Get-ing and
// Put-ing that connection for you. If an error is returned when Get-ing a
// client that is returned instead of calling the function
func (p *Pool) With(f func(*redis.Client)) error {
	client, err := p.Get()
	if err != nil {
		return err
	}
	f(client)
	p.Put(client)
	return nil
}
