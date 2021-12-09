# Docker Graylog Driver

## Description

A docker log driver that writes fields to graylog. In the container, logs must be written in sdtout in gelf format ([https://docs.graylog.org/docs/gelf#gelf-payload-specification](https://docs.graylog.org/docs/gelf)).

## Compile and package

```bash
REGISTRY=xxx make build
```

## Create the plugin

```bash
REGISTRY=xxx make plugin
```

## Usage

Create a `Dockerfile` containing:

```docker
FROM debian:buster

CMD [ "echo", "{\"_application\":\"test\",\"_application_uuid\":\"67e57b2f-4b63-44e7-90b6-35672bd41bb4\",\"_pid\":1,\"level\":6,\"level_name\":\"info\",\"short_message\":\"test is launched\",\"timestamp\":1638907218.9201,\"version\":\"1.1\"}" ]
```

Build the docker:

```bash
docker built -t test .
```

Run it

```bash
docker run  --log-driver ${REGISTRY}/graylogdriver:latest --log-opt gelf-address=udp://0.0.0.0:12201 test
```
