package dockerx

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
)

const (
	Image         = "hashicorp/http-echo"
	Tag           = "latest"
	ContainerPort = "5678/tcp"
	Label         = "aurelius.instance"
)

type Client struct {
	http *http.Client
}

func NewClient(socket string) *Client {
	return &Client{http: &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				return (&net.Dialer{}).DialContext(ctx, "unix", socket)
			},
		},
	}}
}

func ContainerName(id int) string {
	return fmt.Sprintf("aurelius-instance-%d", id)
}

func (c *Client) do(method string, path string, body any) (int, []byte, error) {
	var reader io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			return 0, nil, err
		}
		reader = bytes.NewReader(payload)
	}

	req, err := http.NewRequest(method, "http://docker"+path, reader)
	if err != nil {
		return 0, nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	res, err := c.http.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer res.Body.Close()

	data, err := io.ReadAll(res.Body)
	return res.StatusCode, data, err
}

func (c *Client) PullImage() error {
	code, _, err := c.do("GET", "/images/"+Image+":"+Tag+"/json", nil)
	if err != nil {
		return err
	}
	if code == http.StatusOK {
		return nil
	}

	code, data, err := c.do("POST", "/images/create?fromImage="+Image+"&tag="+Tag, nil)
	if err != nil {
		return err
	}
	if code != http.StatusOK {
		return fmt.Errorf("pull %s:%s: %d %s", Image, Tag, code, data)
	}
	return nil
}

func (c *Client) RunContainer(id int, name string, port int) error {
	container := map[string]any{
		"Image":        Image + ":" + Tag,
		"Cmd":          []string{"-text=Hello from " + name},
		"Labels":       map[string]string{Label: strconv.Itoa(id)},
		"ExposedPorts": map[string]any{ContainerPort: map[string]any{}},
		"HostConfig": map[string]any{
			"PortBindings": map[string]any{
				ContainerPort: []map[string]string{{"HostIp": "0.0.0.0", "HostPort": strconv.Itoa(port)}},
			},
		},
	}

	code, data, err := c.do("POST", "/containers/create?name="+ContainerName(id), container)
	if err != nil {
		return err
	}
	if code != http.StatusCreated {
		return fmt.Errorf("create %s: %d %s", ContainerName(id), code, data)
	}

	code, data, err = c.do("POST", "/containers/"+ContainerName(id)+"/start", nil)
	if err != nil {
		return err
	}
	if code != http.StatusNoContent && code != http.StatusNotModified {
		return fmt.Errorf("start %s: %d %s", ContainerName(id), code, data)
	}
	return nil
}

func (c *Client) RemoveContainer(id int) error {
	code, data, err := c.do("DELETE", "/containers/"+ContainerName(id)+"?force=true", nil)
	if err != nil {
		return err
	}
	if code != http.StatusNoContent && code != http.StatusNotFound {
		return fmt.Errorf("remove %s: %d %s", ContainerName(id), code, data)
	}
	return nil
}
