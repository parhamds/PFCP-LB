// SPDX-License-Identifier: Apache-2.0
// Copyright 2022-present Open Networking Foundation

package pfcpiface

import (
	"context"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os/exec"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
	"github.com/wmnsk/go-pfcp/ie"
	"github.com/wmnsk/go-pfcp/message"
)

type Position int

type CommunicationChannel struct {
	U2d           chan []byte
	D2u           chan []byte
	UpfD2u        chan *PfcpInfo
	SesEstU2d     chan *SesEstU2dMsg
	SesModU2d     chan *SesModU2dMsg
	SesDelU2d     chan *SesDelU2dMsg
	ResetSessions chan struct{}
}

type SesEstU2dMsg struct {
	msg       *message.SessionEstablishmentRequest
	upSeid    uint64
	reforward bool
	respCh    chan *ie.IE
}

type SesModU2dMsg struct {
	msg       *message.SessionModificationRequest
	upSeid    uint64
	reforward bool
	respCh    chan *ie.IE
}

type SesDelU2dMsg struct {
	msg       *message.SessionDeletionRequest
	upSeid    uint64
	reforward bool
	respCh    chan *ie.IE
	upfIndex  int
	pConn     *PFCPConn
}

//type Sessionsinfo struct {
//	LSeidUp      uint64
//	LSeidDown    uint64
//	RealPFCPSeid uint64
//}

//type SessionMap map[uint64]*Sessionsinfo

const (
	Up Position = iota
	Down
)

var (
	simulate = simModeDisable
)

func init() {
	flag.Var(&simulate, "simulate", "create|delete|create_continue simulated sessions")
}

type PFCPIface struct {
	conf Conf

	node *PFCPNode
	fp   datapath
	upf  *Upf

	httpSrv      *http.Server
	httpEndpoint string

	uc *upfCollector
	nc *PfcpNodeCollector

	mu sync.Mutex
}

func RunUPFs(conf *Conf) error {
	var upfName string
	for i := 1; i <= int(conf.InitUPFs); i++ {
		if i < 10 {
			upfName = fmt.Sprint("upf10", i)
		} else if i < 100 {
			upfName = fmt.Sprint("upf1", i)
		} else if i >= 100 {
			upfName = fmt.Sprint("upf", i)
		}
		upfFile := fmt.Sprint("/upfs/", upfName, ".yaml")
		cmd := exec.Command("kubectl", "apply", "-n", "omec", "-f", upfFile)
		log.Traceln("executing command : ", cmd.String())
		combinedOutput, err := cmd.CombinedOutput()
		if err != nil {
			fmt.Printf("Error executing command: %v\nCombined Output: %s", cmd.String(), combinedOutput)
			return err
		}
		time.Sleep(2 * time.Second)
	}
	return nil
}

func NewPFCPIface(conf Conf, pos Position) *PFCPIface {
	pfcpIface := &PFCPIface{
		conf: conf,
	}

	//if conf.EnableP4rt {
	//	pfcpIface.fp = &UP4{}
	//} else {
	//	pfcpIface.fp = &bess{}
	//}
	var httpPort string
	if pos == Up {
		httpPort = "8080"
	} else {
		httpPort = "8081"
	}

	if conf.CPIface.HTTPPort != "" {
		httpPort = conf.CPIface.HTTPPort
	}

	pfcpIface.httpEndpoint = ":" + httpPort

	pfcpIface.upf = NewUPF(&conf, pos) //pfcpIface.fp

	return pfcpIface
}

func listenForUpf(comCh CommunicationChannel, upf *Upf) {
	for {
		exist := false
		newPfcpInfo := <-comCh.UpfD2u
		for _, u := range upf.peersUPF {
			if u.NodeID == newPfcpInfo.Upf.NodeID {
				exist = true
			}
		}
		if !exist {
			newPfcpInfo.Upf.peersIP = newPfcpInfo.Ip
			upf.peersUPF = append(upf.peersUPF, newPfcpInfo.Upf)
		}
	}
}

func (p *PFCPIface) mustInit(comCh CommunicationChannel, pos Position) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if pos == Up {
		//fmt.Println("parham log: calling NewPFCPNode for up")
	} else {
		//fmt.Println("parham log: calling NewPFCPNode for down")
	}

	p.node = NewPFCPNode(pos, p.upf) //p.upf,

	if pos == Up {
		go listenForUpf(comCh, p.node.upf)
	}

	//var err error

	//p.uc, p.nc, err = setupProm(httpMux, p.upf, p.node)

	//if err != nil {
	//	log.Fatalln("setupProm failed", err)
	//}

	//if pos == Down {
	//	httpMux := http.NewServeMux()
	//	setupConfigHandler(httpMux, p.upf)
	//	p.httpSrv = &http.Server{Addr: p.httpEndpoint, Handler: httpMux, ReadHeaderTimeout: 60 * time.Second}
	//}

}

func (p *PFCPIface) Run(comch CommunicationChannel, pos Position) {
	if simulate.enable() {
		p.upf.sim(simulate, &p.conf.SimInfo)

		//fmt.Println("parham log : simulate.enable() is true")

		if !simulate.keepGoing() {
			return
		}
	}
	if pos == Up {
		//fmt.Println("parham log: calling mustInit for up")
	} else {
		//fmt.Println("parham log: calling mustInit for down")
	}
	p.mustInit(comch, pos)

	if pos == Down {
		//time.Sleep(10 * time.Minute)
		go p.node.listenForSesEstReq(comch)
		go p.node.listenForSesModReq(comch)
		go p.node.listenForSesDelReq(comch)
		go p.node.listenForResetSes(comch)
		if p.node.upf.AutoScaleIn || p.node.upf.AutoScaleOut {
			go p.node.reconciliation(comch)
		}
		http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			newPFCPHandler(w, r, p.node, comch, pos)
		})
		http.HandleFunc("/trans-ses", func(w http.ResponseWriter, r *http.Request) {
			sesTransHandler(w, r, p.node, comch, pos)
		})
		http.HandleFunc("/del-upf", func(w http.ResponseWriter, r *http.Request) {
			upfDelHandler(w, r, p.node, comch, pos)
		})
		server := http.Server{Addr: ":8081"}
		go func() {
			//fmt.Println("parham log : http server is serving")
			//if err := p.httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			//	log.Fatalln("http server failed", err)
			//}
			////log.infoln("http server closed")
			//http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			//	newPFCPHandler(w, r, p.upf)
			//})
			//fmt.Println("Server started on :8081")
			//http.ListenAndServe(":8081", nil)
			server.ListenAndServe()
		}()

		//sig := make(chan os.Signal, 1)
		//signal.Notify(sig, os.Interrupt)
		//signal.Notify(sig, syscall.SIGTERM)

		//go func() {
		//	oscall := <-sig
		//	//log.infof("System call received: %+v", oscall)
		//	server.Shutdown(nil)
		//}()
		//go func() {
		//	oscall := <-sig
		//	//log.infof("System call received: %+v", oscall)
		//	p.Stop()
		//}()
	}
	//time.Sleep(1 * time.Minute)
	// blocking

	if pos == Up {
		//fmt.Println("parham log: calling Serve for up")
	} else {
		//fmt.Println("parham log: calling Serve for down")
	}
	p.node.Serve(comch, pos)
}

// Stop sends cancellation signal to main Go routine and waits for shutdown to complete.
func (p *PFCPIface) Stop() {
	p.mu.Lock()
	defer p.mu.Unlock()

	ctxHttpShutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer func() {
		cancel()
	}()

	if err := p.httpSrv.Shutdown(ctxHttpShutdown); err != nil {
		log.Errorln("Failed to shutdown http: ", err)
	}

	p.node.Stop()

	// Wait for PFCP node shutdown
	p.node.Done()
}

// GetLocalIP returns ip of first non loopback interface in string
func GetLocalIP() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return ""
	}
	for _, address := range addrs {
		// check the address type and if it is not a loopback the display it
		if ipnet, ok := address.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ipnet.IP.To4() != nil {
				return ipnet.IP.String()
			}
		}
	}
	return ""
}
