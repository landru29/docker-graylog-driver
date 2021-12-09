package main

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/containerd/fifo"
	"github.com/docker/docker/api/types/plugins/logdriver"
	"github.com/docker/docker/daemon/logger"
	"github.com/docker/docker/daemon/logger/loggerutils"
	"github.com/docker/docker/pkg/urlutil"
	protoio "github.com/gogo/protobuf/io"
	"github.com/pkg/errors"
	"gopkg.in/Graylog2/go-gelf.v1/gelf"
)

type driver struct {
	mu   sync.Mutex
	logs map[string]*dockerInput
}

type dockerInput struct {
	stream   io.ReadCloser
	info     logger.Info
	gelf     *gelf.Writer
	hostname string
	extra    map[string]interface{}
}

func (d dockerInput) Close() error {
	err := d.gelf.Close()
	if err != nil {
		return err
	}

	return d.stream.Close()
}

func newDriver() *driver {
	return &driver{
		logs: make(map[string]*dockerInput),
	}
}

func (d *driver) StartLogging(file string, logCtx logger.Info) error {
	d.mu.Lock()
	if _, exists := d.logs[file]; exists {
		d.mu.Unlock()
		return fmt.Errorf("logger for %q already exists", file)
	}
	d.mu.Unlock()

	f, err := fifo.OpenFifo(context.Background(), file, syscall.O_RDONLY, 0700)
	if err != nil {
		return errors.Wrapf(err, "error opening logger fifo: %q", file)
	}

	graylogAddr, err := parseAddress(logCtx.Config["gelf-address"])
	if err != nil {
		return err
	}

	// parse log tag
	tag, err := loggerutils.ParseLogTag(logCtx, loggerutils.DefaultTemplate)
	if err != nil {
		return err
	}

	// collect extra data for GELF message
	hostname, err := logCtx.Hostname()
	if err != nil {
		return fmt.Errorf("gelf: cannot access hostname to set source field")
	}

	var gelfWriter *gelf.Writer
	if graylogAddr.Scheme == "udp" {
		gelfWriter, err = newGELFUDPWriter(graylogAddr.Host, logCtx)
		if err != nil {
			return err
		}
	}

	d.mu.Lock()
	lf := &dockerInput{
		stream:   f,
		info:     logCtx,
		gelf:     gelfWriter,
		hostname: hostname,
		extra: map[string]interface{}{
			"_container_id":   logCtx.ContainerID,
			"_container_name": logCtx.Name(),
			"_image_id":       logCtx.ContainerImageID,
			"_image_name":     logCtx.ContainerImageName,
			"_command":        logCtx.Command(),
			"_tag":            tag,
			"_created":        logCtx.ContainerCreated,
		},
	}
	d.logs[file] = lf
	d.mu.Unlock()
	d.PrintState()
	go consumeLog(lf)
	return nil
}

func (d *driver) PrintState() {
	fmt.Fprintln(os.Stdout, "New Container added for logging : >")
	for k, v := range d.logs {
		fmt.Fprintf(os.Stdout, " %s = %s\n", k, v.info.ContainerID)
	}
}

func (d *driver) StopLogging(file string) error {
	d.mu.Lock()
	lf, ok := d.logs[file]
	if ok {
		lf.stream.Close()
		delete(d.logs, file)
	}
	d.mu.Unlock()
	return nil
}

func consumeLog(lf *dockerInput) {
	lf.gelf.WriteMessage(&gelf.Message{
		Version: "1.1",
		Host:    lf.hostname,
		Short:   "starting logging",
		Level:   6,
	})
	dec := protoio.NewUint32DelimitedReader(lf.stream, binary.BigEndian, 1e6)
	defer dec.Close()
	defer lf.Close()
	var buf logdriver.LogEntry
	for {
		buf.Reset()
		if err := dec.ReadMsg(&buf); err != nil {
			if err == io.EOF {
				lf.gelf.WriteMessage(&gelf.Message{
					Version: "1.1",
					Host:    lf.hostname,
					Short:   fmt.Sprintf("FIFO Stream closed  %s", err),
					Level:   3,
				})
				return
			}
			dec = protoio.NewUint32DelimitedReader(lf.stream, binary.BigEndian, 1e6)
		}

		if len(buf.Line) < 10 {
			continue
		}

		data := map[string]interface{}{}
		err := json.Unmarshal(buf.Line, &data)
		if err != nil {
			continue
		}

		message := "no message"
		if msg, ok := data["short_message"].(string); ok {
			message = msg
		}

		version := "1.1"
		if ver, ok := data["version"].(string); ok {
			version = ver
		}

		level := int32(6)
		if lev, ok := data["level"].(int32); ok {
			level = lev
		}

		extra := map[string]interface{}{}
		for key, val := range lf.extra {
			extra[key] = val
		}

		for key, val := range data {
			if key[0] == '_' {
				extra[key] = val
			}
		}

		if err := lf.gelf.WriteMessage(&gelf.Message{
			Version:  version,
			Host:     lf.hostname,
			Short:    message,
			TimeUnix: float64(buf.TimeNano/int64(time.Millisecond)) / 1000.0,
			Level:    level,
			Extra:    extra,
		}); err != nil {
			lf.gelf.WriteMessage(&gelf.Message{
				Version: "1.1",
				Host:    lf.hostname,
				Short:   fmt.Sprintf("gelf send  %s", err.Error()),
				Level:   6,
			})

			continue
		}
	}
}

func (d *driver) ReadLogs(info logger.Info, config logger.ReadConfig) (io.ReadCloser, error) {
	return nil, nil
}

func parseAddress(address string) (*url.URL, error) {
	if address == "" {
		return nil, fmt.Errorf("gelf-address is a required parameter")
	}
	if !urlutil.IsTransportURL(address) {
		return nil, fmt.Errorf("gelf-address should be in form proto://address, got %v", address)
	}
	url, err := url.Parse(address)
	if err != nil {
		return nil, err
	}

	// we support only udp
	if url.Scheme != "udp" {
		return nil, fmt.Errorf("gelf: endpoint needs to be UDP")
	}

	// get host and port
	if _, _, err = net.SplitHostPort(url.Host); err != nil {
		return nil, fmt.Errorf("gelf: please provide gelf-address as proto://host:port")
	}

	return url, nil
}

// create new UDP gelfWriter
func newGELFUDPWriter(address string, info logger.Info) (*gelf.Writer, error) {
	gelfWriter, err := gelf.NewWriter(address)
	if err != nil {
		return nil, fmt.Errorf("gelf: cannot connect to GELF endpoint: %s %v", address, err)
	}

	if v, ok := info.Config["gelf-compression-type"]; ok {
		switch v {
		case "gzip":
			gelfWriter.CompressionType = gelf.CompressGzip
		case "zlib":
			gelfWriter.CompressionType = gelf.CompressZlib
		case "none":
			gelfWriter.CompressionType = gelf.CompressNone
		default:
			return nil, fmt.Errorf("gelf: invalid compression type %q", v)
		}
	}

	if v, ok := info.Config["gelf-compression-level"]; ok {
		val, err := strconv.Atoi(v)
		if err != nil {
			return nil, fmt.Errorf("gelf: invalid compression level %s, err %v", v, err)
		}
		gelfWriter.CompressionLevel = val
	}

	return gelfWriter, nil
}
