// Copyright 2016 Netflix, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package httph_test

import (
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/netflix/rend-http/httph"
	"github.com/netflix/rend/common"
	"github.com/netflix/rend/handlers"
)

type server struct {
	data      map[string]string
	forcecode int
}

func newServer(forcecode int) *server {
	return &server{
		data:      make(map[string]string),
		forcecode: forcecode,
	}
}

func (s *server) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	key := strings.TrimPrefix(req.URL.Path, "/evcrest/v1.0/evcache/")

	if s.forcecode > 0 {
		w.WriteHeader(s.forcecode)
		return
	}

	switch req.Method {
	case "GET":
		if data, ok := s.data[key]; ok {
			w.Header().Set("Content-Type", "application/octet-stream")
			w.Write([]byte(data))
		} else {
			w.WriteHeader(404)
		}

	case "POST":
		fallthrough
	case "PUT":
		// verify ttl
		ttl := req.URL.Query().Get("ttl")
		if ttl == "" {
			w.WriteHeader(400)
		}
		if _, err := strconv.Atoi(ttl); err != nil {
			w.WriteHeader(400)
		}

		// Don't bother with TTL here for testing
		data, err := ioutil.ReadAll(req.Body)
		if err != nil {
			w.WriteHeader(500)
		}
		s.data[key] = string(data)
		w.WriteHeader(200)

	case "DELETE":
		delete(s.data, key)
		w.WriteHeader(200)
	}
}

func handlerFromTestServer(ts *httptest.Server) handlers.Handler {
	hostAndPort := strings.TrimPrefix(ts.URL, "http://")
	parts := strings.Split(hostAndPort, ":")

	host := parts[0]
	port, err := strconv.Atoi(parts[1])
	if err != nil {
		panic(err)
	}

	handler, err := httph.New(host, port, "evcache")()
	if err != nil {
		panic(fmt.Sprintf("Handler creation failed: %s", err.Error()))
	}

	return handler
}

func TestGetHit(t *testing.T) {
	s := newServer(0)
	ts := httptest.NewServer(s)
	defer ts.Close()

	handler := handlerFromTestServer(ts)

	// somewhat dirty, but I don't mind
	s.data["foo"] = "bar"

	datchan, errchan := handler.Get(common.GetRequest{
		Keys:    [][]byte{[]byte("foo")},
		Opaques: []uint32{0},
		Quiet:   []bool{false},
	})

	select {
	case res := <-datchan:
		if !res.Miss {
			t.Log("Success, got item")
			t.Logf("Value: %s", string(res.Data))
		} else {
			t.Error("Response was a miss")
		}
	case err := <-errchan:
		t.Errorf("Failed to retrieve item: %s", err.Error())
	}
}

func TestGetMiss(t *testing.T) {
	s := newServer(0)
	ts := httptest.NewServer(s)
	defer ts.Close()

	handler := handlerFromTestServer(ts)

	datchan, errchan := handler.Get(common.GetRequest{
		Keys:    [][]byte{[]byte("foo")},
		Opaques: []uint32{0},
		Quiet:   []bool{false},
	})

	select {
	case res := <-datchan:
		if res.Miss {
			t.Log("Success, miss")
			t.Logf("Value: %s", string(res.Data))
		} else {
			t.Error("Response was a hit")
		}
	case err := <-errchan:
		t.Errorf("Failed to retrieve item: %s", err.Error())
	}
}

func TestGetError(t *testing.T) {
	s := newServer(500)
	ts := httptest.NewServer(s)
	defer ts.Close()

	handler := handlerFromTestServer(ts)

	datchan, errchan := handler.Get(common.GetRequest{
		Keys:    [][]byte{[]byte("foo")},
		Opaques: []uint32{0},
		Quiet:   []bool{false},
	})

	select {
	case res := <-datchan:
		t.Errorf("Should have received an error.\nResponse: %#v", res)
	case err := <-errchan:
		t.Logf("Properly received error: %s", err.Error())
	}
}

func TestSetSuccess(t *testing.T) {
	s := newServer(0)
	ts := httptest.NewServer(s)
	defer ts.Close()

	handler := handlerFromTestServer(ts)

	err := handler.Set(common.SetRequest{
		Key:  []byte("foo"),
		Data: []byte("bar"),
	})

	if err != nil {
		t.Errorf("Failed set request: %s", err.Error())
	}

	if data, ok := s.data["foo"]; ok {
		if data == "bar" {
			t.Logf("Successfully performed set")
		} else {
			t.Errorf("Set data does not match: %s", data)
		}
	} else {
		t.Errorf("No data was set")
	}
}

func TestSetError(t *testing.T) {
	s := newServer(500)
	ts := httptest.NewServer(s)
	defer ts.Close()

	handler := handlerFromTestServer(ts)

	err := handler.Set(common.SetRequest{
		Key:  []byte("foo"),
		Data: []byte("bar"),
	})

	if err != nil {
		t.Logf("Properly received error: %s", err.Error())
	} else {
		t.Errorf("Should have received an error.")
	}
}

func TestSetBadRequest(t *testing.T) {
	s := newServer(400)
	ts := httptest.NewServer(s)
	defer ts.Close()

	handler := handlerFromTestServer(ts)

	err := handler.Set(common.SetRequest{
		Key:  []byte("foo"),
		Data: []byte("bar"),
	})

	if err != nil {
		t.Logf("Properly received error: %s", err.Error())
	} else {
		t.Errorf("Should have received an error.")
	}
}

func TestDeleteSuccess(t *testing.T) {
	s := newServer(0)
	ts := httptest.NewServer(s)
	defer ts.Close()

	s.data["foo"] = "bar"

	handler := handlerFromTestServer(ts)

	err := handler.Delete(common.DeleteRequest{
		Key: []byte("foo"),
	})

	if err != nil {
		t.Errorf("Failed delete request: %s", err.Error())
	}

	if data, ok := s.data["foo"]; ok {
		t.Error("Delete failed. Data: %s", data)
	} else {
		t.Log("Delete successful")
	}
}

func TestDeleteError(t *testing.T) {
	s := newServer(500)
	ts := httptest.NewServer(s)
	defer ts.Close()

	handler := handlerFromTestServer(ts)

	err := handler.Delete(common.DeleteRequest{
		Key: []byte("foo"),
	})

	if err != nil {
		t.Logf("Properly received error: %s", err.Error())
	} else {
		t.Errorf("Should have received an error.")
	}
}
