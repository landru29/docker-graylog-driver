# Plugin parameters
PLUGIN_NAME=$(REGISTRY)/graylogdriver
GIT_CURRENT_HASH=$(shell git log -n1 --pretty='%h')
TAG=$(shell git describe --exact-match --tags $(GIT_CURRENT_HASH) 2>/dev/null || echo "latest")
ifneq ($(strip $(TAG)),)
	TAG:=:$(TAG)
endif

all: create-plugin

clean:
	@echo "Removing plugin build directory"
	rm -rf ./plugin-build

build: clean
	@echo "docker build: rootfs image with the plugin"
	docker build -f Dockerfile.build -t ${PLUGIN_NAME}:rootfsimg .
	@echo "### create rootfs directory in ./plugin-build/rootfs"
	mkdir -p ./plugin-build/rootfs
	docker create --name rootfsctr ${PLUGIN_NAME}:rootfsimg
	docker export rootfsctr | tar -x -C ./plugin-build/rootfs
	@echo "### copy config.json to ./plugin-build/"
	cp config.json ./plugin-build/
	docker rm -vf rootfsctr

plugin: build
	@echo "### remove existing plugin ${PLUGIN_NAME}${TAG} if exists"
	docker plugin rm -f ${PLUGIN_NAME}${TAG} || true
	@echo "### create new plugin ${PLUGIN_NAME}${TAG} from ./plugin-build"
	docker plugin create ${PLUGIN_NAME}${TAG} ./plugin-build
	@echo "### enable plugin ${PLUGIN_NAME}${TAG}"
	docker plugin enable ${PLUGIN_NAME}${TAG}

publish: plugin
	docker plugin push ${PLUGIN_NAME}${TAG}


	
