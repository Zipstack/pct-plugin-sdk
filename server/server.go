package server

import (
	"fmt"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"

	"golang.org/x/text/cases"
	"golang.org/x/text/language"

	"github.com/zipstack/pct-plugin-framework/fwhelpers"
	"github.com/zipstack/pct-plugin-framework/schema"

	"github.com/valyala/gorpc"
)

const (
	minPort = 20386
	maxPort = 40386
)

func init() {
	// Register all types before instantiating server.
	gorpc.RegisterType(&schema.StringAttribute{})
	gorpc.RegisterType(&schema.IntAttribute{})
	gorpc.RegisterType(&schema.FloatAttribute{})
	gorpc.RegisterType(&schema.BoolAttribute{})
	gorpc.RegisterType(&schema.ListAttribute{})
	gorpc.RegisterType(&schema.MapAttribute{})
	gorpc.RegisterType(&schema.NestedBlock{})

	gorpc.RegisterType(&schema.ServiceRequest{})
	gorpc.RegisterType(&schema.ServiceResponse{})
}

func addServices(d *gorpc.Dispatcher, pService func() schema.ProviderService, rServices []func() schema.ResourceService) error {
	caser := cases.Title(language.Und)
	p := pService()

	// Add provider service.
	pSvcName := "Provider"
	d.AddService(pSvcName, p)

	// Add resource services.
	// Verify and generate the service name from resource metadata.
	resourceServices := make(map[string]string)
	pTypeName := p.Metadata(&schema.ServiceRequest{}).TypeName
	if pTypeName == "" {
		return fmt.Errorf("Empty name in provider metadata")
	}
	pPrefix := fmt.Sprintf("%s_", pTypeName)

	for _, rService := range rServices {
		r := rService()

		rTypeName := r.Metadata(&schema.ServiceRequest{TypeName: pTypeName}).TypeName
		if !strings.HasPrefix(rTypeName, pPrefix) {
			return fmt.Errorf("Resource %s name should begin with \"%s_", rTypeName, pTypeName)
		}
		rType := strings.TrimPrefix(rTypeName, pPrefix)
		if rType == "" {
			return fmt.Errorf("Resource %s name is incomplete", rTypeName)
		}

		rSvcName := fmt.Sprintf("%sResource", caser.String(rType))
		resourceServices[rTypeName] = rSvcName
		d.AddService(rSvcName, r)
	}

	// Save generated resource service mappings.
	p.UpdateResourceServices(resourceServices)

	return nil
}

func Serve(pService func() schema.ProviderService, rServices []func() schema.ResourceService) {
	logger := fwhelpers.GetLogger()

	// Set error logger.
	gorpc.SetErrorLogger(logger.Printf)

	d := gorpc.NewDispatcher()

	err := addServices(d, pService, rServices)
	if err != nil {
		panic(fmt.Sprintf("Failed to add provider services: %s", err.Error()))
	}

	var server *gorpc.Server
	var addr string
	var waitGroup sync.WaitGroup

	logger.Printf("Starting provider server.")
	waitGroup.Add(1)

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGTERM)

	go func() {
		for sig := range sigs {
			logger.Printf("Received signal: %s", sig)

			if sig == syscall.SIGTERM {
				server.Stop()
				waitGroup.Done()
			}
		}
	}()

	for port := minPort; port <= maxPort; port++ {
		addr = fmt.Sprintf("127.0.0.1:%d", port)

		server = gorpc.NewTCPServer(addr, d.NewHandlerFunc())
		if err := server.Start(); err != nil {
			logger.Printf("Unable to bind listener: %s", err.Error())
			continue
		} else {
			break
		}
	}

	if server == nil {
		panic("Failed to start provider server")
	} else {
		logger.Printf("pctctl|addr|%s", addr)
		waitGroup.Wait()
	}
}