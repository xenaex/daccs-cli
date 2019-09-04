PLATFORMS := linux-386 linux-amd64 windows-386 windows-amd64 darwin-amd64

pf = $(subst -, ,$@)
os = $(word 1, $(pf))
arch = $(word 2, $(pf))
dir = 'daccs-cli-$(os)-$(arch)-$(tag)'
ext = $(if $(filter $(os),windows),.exe,)

release: $(PLATFORMS)

$(PLATFORMS):
	GOOS=$(os) GOARCH=$(arch) go build -o build/$(dir)/daccs-cli$(ext)
	tar -czvf build/$(dir).tar.gz build/$(dir) 
	shasum -a 256 -b build/*.tar.gz > build/checksum 

.PHONY: release $(PLATFORMS)