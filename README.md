# Docker Graylog Driver

## Description

A docker log driver that writes fields to graylog. In the container, logs must be written in sdtout in gelf format ([https://docs.graylog.org/docs/gelf#gelf-payload-specification](https://docs.graylog.org/docs/gelf)).

## Compile and package

```bash
make build
```

## Create the plugin

```bash
make create-plugin
```
