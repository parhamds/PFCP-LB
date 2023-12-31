// SPDX-License-Identifier: Apache-2.0
// Copyright 2020 Intel Corporation

package pfcpiface

import (
	"errors"
	"net"
	"time"

	"github.com/Showmax/go-fqdn"
	log "github.com/sirupsen/logrus"
	"github.com/wmnsk/go-pfcp/ie"
	"github.com/wmnsk/go-pfcp/message"
)

// QosConfigVal : Qos configured value.
type QosConfigVal struct {
	cbs              uint32
	pbs              uint32
	ebs              uint32
	burstDurationMs  uint32
	schedulePriority uint32
}

type SliceInfo struct {
	name         string
	uplinkMbr    uint64
	downlinkMbr  uint64
	ulBurstBytes uint64
	dlBurstBytes uint64
	ueResList    []UeResource
}

type UeResource struct {
	name string
	dnn  string
}
type Upf struct {
	EnableUeIPAlloc        bool `json:"enableueipalloc"`
	EnableEndMarker        bool `json:"enableendmarker"`
	enableFlowMeasure      bool
	accessIface            string
	coreIface              string
	ippoolCidr             string
	AccessIP               net.IP `json:"accessip"`
	CoreIP                 net.IP `json:"coreip"`
	NodeID                 string `json:"nodeid"`
	ippool                 *IPPool
	peersIP                string
	peersUPF               []*Upf
	upfsSessions           []uint64       // each upf handles which sessions
	lbmap                  map[uint64]int // each session is handled by which upf
	sesEstMsgStore         map[uint64]*message.SessionEstablishmentRequest
	sesModMsgStore         map[uint64]*message.SessionModificationRequest
	seidToRespCh           map[uint64]chan *ie.IE
	MaxSessionsThreshold   uint32
	MinSessionsThreshold   uint32
	MaxCPUThreshold        uint32
	MinCPUThreshold        uint32
	MaxBitRateThreshold    uint64
	MinBitRateThreshold    uint64
	MaxSessionstolerance   float32
	MinSessionstolerance   float32
	ReconciliationInterval uint32
	AutoScaleOut           bool
	AutoScaleIn            bool
	ScaleByCPU             bool
	ScaleBySession         bool
	ScaleByBitRate         bool
	MaxUPFs                uint32
	MinUPFs                uint32
	Hostname               string `json:"hostname"`
	LastBytes              uint64
	ScaleInDecision        bool
	//peersSessions     []SessionMap
	Dnn              string `json:"dnn"`
	reportNotifyChan chan uint64
	sliceInfo        *SliceInfo
	readTimeout      time.Duration

	datapath
	maxReqRetries uint8
	respTimeout   time.Duration
	enableHBTimer bool
	hbInterval    time.Duration
}

// to be replaced with go-pfcp structs

// Don't change these values.
const (
	tunnelGTPUPort = 2152

	// src-iface consts.
	core   = 0x2
	access = 0x1

	// far-id specific directions.
	n3 = 0x0
	n6 = 0x1
	n9 = 0x2
)

func (u *Upf) isConnected() bool {
	return u.datapath.IsConnected(&u.AccessIP)
}

func (u *Upf) addSliceInfo(sliceInfo *SliceInfo) error {
	if sliceInfo == nil {
		return ErrInvalidArgument("sliceInfo", sliceInfo)
	}

	u.sliceInfo = sliceInfo

	return u.datapath.AddSliceInfo(sliceInfo)
}

func (u *Upf) addPFCPPeer(pfcpInfo *PfcpInfo) error {
	if pfcpInfo == nil {
		return errors.New("invalid PFCP peer IP")
	}
	//fmt.Println("parhamlog : recieved upf info :")
	//fmt.Println("upf info :")
	//fmt.Println("dnn = ", pfcpInfo.Upf.Dnn)
	//fmt.Println("accessIP = ", pfcpInfo.Upf.AccessIP)
	//fmt.Println("coreIP = ", pfcpInfo.Upf.CoreIP)
	//fmt.Println("nodeID = ", pfcpInfo.Upf.NodeID)
	pfcpInfo.Upf.peersIP = pfcpInfo.Ip
	pfcpInfo.Upf.upfsSessions = make([]uint64, 0)
	u.peersUPF = append(u.peersUPF, pfcpInfo.Upf)

	//u.peersSessions = append(u.peersSessions, SessionMap{})
	//fmt.Println("peer added to Down PFCP. list of peers : ", u.peersIP)
	return nil
}

func NewUPF(conf *Conf, pos Position,

// fp datapath
) *Upf {
	var (
		err    error
		nodeID string
	)

	nodeID = conf.CPIface.NodeID
	if conf.CPIface.UseFQDN && nodeID == "" {
		nodeID, err = fqdn.FqdnHostname()
		if err != nil {
			log.Fatalln("Unable to get hostname", err)
		}
	}

	// TODO: Delete this once CI config is fixed
	if nodeID != "" {
		hosts, err := net.LookupHost(nodeID)
		if err != nil {
			log.Fatalln("Unable to resolve hostname", nodeID, err)
		}

		nodeID = hosts[0]
	}
	resptime, err := time.ParseDuration(conf.RespTimeout)
	if err != nil {
		log.Errorln("Error Parsing RespTimeout : ")
		return nil
	}
	//fmt.Println("parham log : parsed RespTimeout = ", resptime)

	u := &Upf{
		EnableUeIPAlloc: conf.CPIface.EnableUeIPAlloc,
		EnableEndMarker: conf.EnableEndMarker,
		//enableFlowMeasure: conf.EnableFlowMeasure,
		//accessIface:       conf.AccessIface.IfName,
		//coreIface:         conf.CoreIface.IfName,
		//ippoolCidr:        conf.CPIface.UEIPPool,
		NodeID: nodeID,
		//datapath:          fp,
		Dnn:            conf.CPIface.Dnn,
		peersUPF:       make([]*Upf, 0),
		upfsSessions:   make([]uint64, 0),
		lbmap:          make(map[uint64]int, 0),
		sesEstMsgStore: make(map[uint64]*message.SessionEstablishmentRequest, 0),
		sesModMsgStore: make(map[uint64]*message.SessionModificationRequest, 0),
		seidToRespCh:   make(map[uint64]chan *ie.IE),
		//peersSessions: make([]SessionMap, 0),
		//reportNotifyChan:  make(chan uint64, 1024),
		maxReqRetries:          conf.MaxReqRetries,
		enableHBTimer:          conf.EnableHBTimer,
		readTimeout:            time.Second * time.Duration(conf.ReadTimeout),
		respTimeout:            time.Second * resptime,
		MaxSessionsThreshold:   conf.MaxSessionsThreshold,
		MinSessionsThreshold:   conf.MinSessionsThreshold,
		MaxCPUThreshold:        conf.MaxCPUThreshold,
		MinCPUThreshold:        conf.MinCPUThreshold,
		MaxBitRateThreshold:    conf.MaxBitRateThreshold,
		MinBitRateThreshold:    conf.MinBitRateThreshold,
		MaxSessionstolerance:   conf.MaxSessionstolerance,
		MinSessionstolerance:   conf.MinSessionstolerance,
		ReconciliationInterval: conf.ReconciliationInterval,
		AutoScaleOut:           conf.AutoScaleOut,
		AutoScaleIn:            conf.AutoScaleIn,
		ScaleByCPU:             conf.ScaleByCPU,
		ScaleBySession:         conf.ScaleBySession,
		ScaleByBitRate:         conf.ScaleByBitRate,
		MaxUPFs:                conf.MaxUPFs,
		MinUPFs:                conf.MinUPFs,
		//readTimeout: 15 * time.Second,
	}

	if pos == Down {
		u.enableHBTimer = true
		u.hbInterval = 5 * time.Second
	}

	//if len(conf.CPIface.Peers) > 0 {
	//	u.peers = make([]string, len(conf.CPIface.Peers))
	//	nc := copy(u.peers, conf.CPIface.Peers)
	//
	//	if nc == 0 {
	//		log.Warnln("Failed to parse cpiface peers, PFCP Agent will not initiate connection to N4 peers.")
	//	}
	//}
	//
	if !conf.EnableP4rt {
		u.AccessIP, err = GetUnicastAddressFromInterface(conf.AccessIface.IfName)
		if err != nil {
			log.Errorln(err)
			return nil
		}

		u.CoreIP, err = GetUnicastAddressFromInterface(conf.CoreIface.IfName)
		if err != nil {
			log.Errorln(err)
			return nil
		}
	}
	//
	//u.respTimeout, err = time.ParseDuration(conf.RespTimeout)
	//if err != nil {
	//	log.Fatalln("Unable to parse resp_timeout")
	//}
	//
	//if u.enableHBTimer {
	//	if conf.HeartBeatInterval != "" {
	//		u.hbInterval, err = time.ParseDuration(conf.HeartBeatInterval)
	//		if err != nil {
	//			log.Fatalln("Unable to parse heart_beat_interval")
	//		}
	//	}
	//}
	//
	//if u.enableUeIPAlloc {
	//	u.ippool, err = NewIPPool(u.ippoolCidr)
	//	if err != nil {
	//		log.Fatalln("ip pool init failed", err)
	//	}
	//}
	//
	//u.datapath.SetUpfInfo(u, conf)

	return u
}
