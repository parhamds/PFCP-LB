// SPDX-License-Identifier: Apache-2.0
// Copyright 2020 Intel Corporation

package pfcpiface

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"os/exec"
	"time"

	log "github.com/sirupsen/logrus"
)

// NetworkSlice ... Config received for slice rates and DNN.
type NetworkSlice struct {
	SliceName string      `json:"sliceName"`
	SliceQos  SliceQos    `json:"sliceQos"`
	UeResInfo []UeResInfo `json:"ueResourceInfo"`
}

type PfcpInfo struct {
	Ip  string `json:"ip"`
	Upf *Upf   `json:"upf"`
}

type SesTransReq struct {
	SessId uint64 `json:"sesid"`
	Supf   int    `json:"supf"`
	Dupf   int    `json:"dupf"`
}
type upfDelReq struct {
	UpfId int `json:"upfid"`
}

// SliceQos ... Slice level QOS rates.
type SliceQos struct {
	UplinkMbr    uint64 `json:"uplinkMbr"`
	DownlinkMbr  uint64 `json:"downlinkMbr"`
	BitrateUnit  string `json:"bitrateUnit"`
	UlBurstBytes uint64 `json:"uplinkBurstSize"`
	DlBurstBytes uint64 `json:"downlinkBurstSize"`
}

// UeResInfo ... UE Pool and DNN info.
type UeResInfo struct {
	Dnn  string `json:"dnn"`
	Name string `json:"uePoolId"`
}

type ConfigHandler struct {
	upf *Upf
}

func setupConfigHandler(mux *http.ServeMux, upf *Upf) {
	cfgHandler := ConfigHandler{upf: upf}
	//mux.Handle("/v1/config/network-slices", &cfgHandler)
	mux.Handle("/", &cfgHandler)
}

func newPFCPHandler(w http.ResponseWriter, r *http.Request, node *PFCPNode, comCh CommunicationChannel, pos Position) {

	switch r.Method {
	case "PUT":
		fallthrough
	case "POST":
		body, err := io.ReadAll(r.Body)
		if err != nil {
			log.Errorln("http req read body failed.")
			sendHTTPResp(http.StatusBadRequest, w)
		}

		//log.traceln(string(body))

		//var nwSlice NetworkSlice
		var pfcpInfo PfcpInfo
		//fmt.Println("parham log : http body = ", body)
		err = json.Unmarshal(body, &pfcpInfo)
		if err != nil {
			log.Errorln("Json unmarshal failed for http request")
			sendHTTPResp(http.StatusBadRequest, w)
		}

		//handleSliceConfig(&nwSlice, c.upf)
		handlePFCPConfig(&pfcpInfo, node.upf)

		//fmt.Println("parham log : send real upf to up")
		//comCh.UpfD2u <- &pfcpInfo

		//fmt.Println("parham log : try creating PFCPConn for new PFCP")
		lAddrStr := node.LocalAddr().String()
		go node.tryConnectToN4Peer(lAddrStr, comCh, pfcpInfo, pos)

		sendHTTPResp(http.StatusCreated, w)
	default:
		//log.infoln(w, "Sorry, only PUT and POST methods are supported.")
		sendHTTPResp(http.StatusMethodNotAllowed, w)
	}
}

func sesTransHandler(w http.ResponseWriter, r *http.Request, node *PFCPNode, comCh CommunicationChannel, pos Position) {

	switch r.Method {
	case "PUT":
		fallthrough
	case "POST":
		body, err := io.ReadAll(r.Body)
		if err != nil {
			log.Errorln("http req read body failed.")
			sendHTTPResp(http.StatusBadRequest, w)
		}

		var sesTransReq SesTransReq
		//fmt.Println("parham log : http body = ", body)
		err = json.Unmarshal(body, &sesTransReq)
		if err != nil {
			log.Errorln("Json unmarshal failed for http request")
			sendHTTPResp(http.StatusBadRequest, w)
		}
		var sess []uint64
		sess = append(sess, sesTransReq.SessId)

		transferSessions(sesTransReq.Supf, sesTransReq.Dupf, sess, node, comCh, true)

		sendHTTPResp(http.StatusCreated, w)
	default:
		//log.infoln(w, "Sorry, only PUT and POST methods are supported.")
		sendHTTPResp(http.StatusMethodNotAllowed, w)
	}
}

func upfDelHandler(w http.ResponseWriter, r *http.Request, node *PFCPNode, comCh CommunicationChannel, pos Position) {

	switch r.Method {
	case "PUT":
		fallthrough
	case "POST":
		body, err := io.ReadAll(r.Body)
		if err != nil {
			log.Errorln("http req read body failed.")
			sendHTTPResp(http.StatusBadRequest, w)
		}

		//log.traceln(string(body))

		//var nwSlice NetworkSlice
		var upfDelReq upfDelReq
		//fmt.Println("parham log : http body = ", body)
		err = json.Unmarshal(body, &upfDelReq)
		if err != nil {
			log.Errorln("Json unmarshal failed for http request")
			sendHTTPResp(http.StatusBadRequest, w)
		}

		//handleSliceConfig(&nwSlice, c.upf)
		makeUPFEmpty(node, upfDelReq.UpfId, comCh)
		time.Sleep(2 * time.Second)
		upfName := node.upf.peersUPF[upfDelReq.UpfId].Hostname
		upfFile := fmt.Sprint("/upfs/", upfName, ".yaml")
		cmd := exec.Command("kubectl", "delete", "-n", "omec", "-f", upfFile)
		log.Traceln("executing command : ", cmd.String())
		combinedOutput, err := cmd.CombinedOutput()
		if err != nil {
			fmt.Printf("Error executing command: %v\nCombined Output: %s", cmd.String(), combinedOutput)
			sendHTTPResp(http.StatusBadRequest, w)
		}
		sendHTTPResp(http.StatusCreated, w)
	default:
		//log.infoln(w, "Sorry, only PUT and POST methods are supported.")
		sendHTTPResp(http.StatusMethodNotAllowed, w)
	}
}

func (c *ConfigHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	//log.infoln("parham log : handle http request for /")

	switch r.Method {
	case "PUT":
		fallthrough
	case "POST":
		body, err := io.ReadAll(r.Body)
		if err != nil {
			log.Errorln("http req read body failed.")
			sendHTTPResp(http.StatusBadRequest, w)
		}

		//log.traceln(string(body))

		//var nwSlice NetworkSlice
		var pfcpInfo PfcpInfo

		err = json.Unmarshal(body, &pfcpInfo)
		if err != nil {
			log.Errorln("Json unmarshal failed for http request")
			sendHTTPResp(http.StatusBadRequest, w)
		}

		//handleSliceConfig(&nwSlice, c.upf)
		handlePFCPConfig(&pfcpInfo, c.upf)
		sendHTTPResp(http.StatusCreated, w)
	default:
		//log.infoln(w, "Sorry, only PUT and POST methods are supported.")
		sendHTTPResp(http.StatusMethodNotAllowed, w)
	}
}

func sendHTTPResp(status int, w http.ResponseWriter) {
	w.WriteHeader(status)
	w.Header().Set("Content-Type", "application/json")

	resp := make(map[string]string)

	switch status {
	case http.StatusCreated:
		resp["message"] = "Status Created"
	default:
		resp["message"] = "Failed to add slice"
	}

	jsonResp, err := json.Marshal(resp)
	if err != nil {
		log.Errorln("Error happened in JSON marshal. Err: ", err)
	}

	_, err = w.Write(jsonResp)
	if err != nil {
		log.Errorln("http response write failed : ", err)
	}
}

// calculateBitRates : Default bit rate is Mbps.
func calculateBitRates(mbr uint64, rate string) uint64 {
	var val int64

	switch rate {
	case "bps":
		return mbr
	case "Kbps":
		val = int64(mbr) * KB
	case "Gbps":
		val = int64(mbr) * GB
	case "Mbps":
		fallthrough
	default:
		val = int64(mbr) * MB
	}

	if val > 0 {
		return uint64(val)
	} else {
		return uint64(math.MaxInt64)
	}
}

func handleSliceConfig(nwSlice *NetworkSlice, upf *Upf) {
	//log.infoln("handle slice config : ", nwSlice.SliceName)

	ulMbr := calculateBitRates(nwSlice.SliceQos.UplinkMbr,
		nwSlice.SliceQos.BitrateUnit)
	dlMbr := calculateBitRates(nwSlice.SliceQos.DownlinkMbr,
		nwSlice.SliceQos.BitrateUnit)
	sliceInfo := SliceInfo{
		name:         nwSlice.SliceName,
		uplinkMbr:    ulMbr,
		downlinkMbr:  dlMbr,
		ulBurstBytes: nwSlice.SliceQos.UlBurstBytes,
		dlBurstBytes: nwSlice.SliceQos.DlBurstBytes,
	}

	if len(nwSlice.UeResInfo) > 0 {
		sliceInfo.ueResList = make([]UeResource, 0)

		for _, ueRes := range nwSlice.UeResInfo {
			var ueResInfo UeResource
			ueResInfo.dnn = ueRes.Dnn
			ueResInfo.name = ueRes.Name
			sliceInfo.ueResList = append(sliceInfo.ueResList, ueResInfo)
		}
	}

	err := upf.addSliceInfo(&sliceInfo)

	if err != nil {
		log.Errorln("adding slice info to datapath failed : ", err)
	}
}

func handlePFCPConfig(pfcpInfo *PfcpInfo, upf *Upf) {
	//log.infoln("handle register pfcp agent : ", pfcpInfo.Ip)
	fmt.Println("new PFCP Peer config Received, Peer's IP = ", pfcpInfo.Ip, ", Peer's Hostname", pfcpInfo.Upf.Hostname)
	err := upf.addPFCPPeer(pfcpInfo)
	if err != nil {
		log.Errorln("adding pfcp info to pfcplb failed : ", err)
	}
}
