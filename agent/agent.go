package agent

import (
	"Stowaway/common"
	"Stowaway/node"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
)

var (
	ProxyChan        *common.ProxyChan
	SocksInfo        *common.SocksSetting
	AgentStatus      *common.AgentStatus
	FileDataMap      *common.IntStrMap
	SocksDataChanMap *common.Uint32ChanStrMap
)
var ConnToAdmin net.Conn

func NewAgent(c *common.AgentOptions) {
	AgentStatus = common.NewAgentStatus()
	SocksInfo = common.NewSocksSetting()
	ProxyChan = common.NewProxyChan()
	SocksDataChanMap = common.NewUint32ChanStrMap()
	FileDataMap = common.NewIntStrMap()

	AgentStatus.AESKey = []byte(c.Secret)
	listenPort := c.Listen
	reconn := c.Reconnect
	passive := c.Reverse
	monitor := c.Monitor
	isStartNode := c.IsStartNode
	reusehost := c.ReuseHost
	reuseport := c.ReusePort
	rhostreuse := c.RhostReuse

	if isStartNode && passive == false && reusehost == "" && reuseport == "" {
		go WaitForExit(AgentStatus.Nodeid)
		StartNodeInit(monitor, listenPort, reconn, passive)
	} else if passive == false && reusehost == "" && reuseport == "" {
		go WaitForExit(AgentStatus.Nodeid)
		SimpleNodeInit(monitor, listenPort, rhostreuse)
	} else if isStartNode && passive && reusehost == "" && reuseport == "" {
		go WaitForExit(AgentStatus.Nodeid)
		StartNodeReversemodeInit(monitor, listenPort, passive)
	} else if passive && reusehost == "" && reuseport == "" {
		go WaitForExit(AgentStatus.Nodeid)
		SimpleNodeReversemodeInit(monitor, listenPort)
	} else if reusehost != "" && reuseport != "" && isStartNode {
		go WaitForExit(AgentStatus.Nodeid)
		StartNodeReuseInit(reusehost, reuseport, listenPort, 1)
	} else if reusehost != "" && reuseport != "" {
		go WaitForExit(AgentStatus.Nodeid)
		SimpleNodeReuseInit(reusehost, reuseport, listenPort, 1)
	} else if reuseport != "" && listenPort != "" && isStartNode {
		StartNodeReuseInit(reusehost, reuseport, listenPort, 2)
	} else if reuseport != "" && listenPort != "" {
		SimpleNodeReuseInit(reusehost, reuseport, listenPort, 2)
	}
}

// 初始化代码开始

// 后续想让startnode与simplenode实现不一样的功能，故将两种node实现代码分开写
func StartNodeInit(monitor, listenPort, reConn string, passive bool) {
	var err error
	AgentStatus.Nodeid = common.StartNodeId
	ConnToAdmin, AgentStatus.Nodeid, err = node.StartNodeConn(monitor, listenPort, AgentStatus.Nodeid, AgentStatus.AESKey)
	if err != nil {
		os.Exit(0)
	}
	go common.SendHeartBeatControl(&ConnToAdmin, AgentStatus.Nodeid, AgentStatus.AESKey)
	go HandleStartNodeConn(&ConnToAdmin, monitor, listenPort, reConn, passive, AgentStatus.Nodeid)
	go node.StartNodeListen(listenPort, AgentStatus.Nodeid, AgentStatus.AESKey)
	go PrepareForReOnlineNode()
	for {
		controlConnForLowerNode := <-node.NodeStuff.ControlConnForLowerNodeChan
		NewNodeMessage := <-node.NodeStuff.NewNodeMessageChan
		<-node.NodeStuff.IsAdmin //正常模式启动的节点被连接一定是agent来连接，所以这里不需要判断是否是admin连接
		ProxyChan.ProxyChanToUpperNode <- NewNodeMessage
		if AgentStatus.NotLastOne == false {
			ProxyChan.ProxyChanToLowerNode = make(chan *common.PassToLowerNodeData)
			go HandleConnToLowerNode()
		}
		AgentStatus.NotLastOne = true
		lowerid := <-AgentStatus.WaitForIdAllocate
		go HandleConnFromLowerNode(controlConnForLowerNode, AgentStatus.Nodeid, lowerid)
	}
}

//普通的node节点
func SimpleNodeInit(monitor, listenPort string, rhostreuse bool) {
	var err error
	AgentStatus.Nodeid = common.AdminId
	if !rhostreuse { //连接的节点是否是在reuseport？
		ConnToAdmin, AgentStatus.Nodeid, err = node.StartNodeConn(monitor, listenPort, AgentStatus.Nodeid, AgentStatus.AESKey)
	} else {
		ConnToAdmin, AgentStatus.Nodeid, err = node.StartNodeConnReuse(monitor, listenPort, AgentStatus.Nodeid, AgentStatus.AESKey)
	}
	if err != nil {
		os.Exit(0)
	}
	go common.SendHeartBeatControl(&ConnToAdmin, AgentStatus.Nodeid, AgentStatus.AESKey)
	go HandleSimpleNodeConn(&ConnToAdmin, AgentStatus.Nodeid)
	go node.StartNodeListen(listenPort, AgentStatus.Nodeid, AgentStatus.AESKey)
	go PrepareForReOnlineNode()
	for {
		controlConnForLowerNode := <-node.NodeStuff.ControlConnForLowerNodeChan
		NewNodeMessage := <-node.NodeStuff.NewNodeMessageChan
		<-node.NodeStuff.IsAdmin //正常模式启动的节点被连接一定是agent来连接，所以这里不需要判断是否是admin连接
		ProxyChan.ProxyChanToUpperNode <- NewNodeMessage
		if AgentStatus.NotLastOne == false {
			ProxyChan.ProxyChanToLowerNode = make(chan *common.PassToLowerNodeData)
			go HandleConnToLowerNode()
		}
		AgentStatus.NotLastOne = true
		lowerid := <-AgentStatus.WaitForIdAllocate
		go HandleConnFromLowerNode(controlConnForLowerNode, AgentStatus.Nodeid, lowerid)
	}
}

//reverse mode下的startnode节点
func StartNodeReversemodeInit(monitor, listenPort string, passive bool) {
	AgentStatus.Nodeid = common.StartNodeId
	ConnToAdmin, AgentStatus.Nodeid = node.AcceptConnFromUpperNode(listenPort, AgentStatus.Nodeid, AgentStatus.AESKey)
	go common.SendHeartBeatControl(&ConnToAdmin, AgentStatus.Nodeid, AgentStatus.AESKey)
	go HandleStartNodeConn(&ConnToAdmin, monitor, listenPort, "", passive, AgentStatus.Nodeid)
	go node.StartNodeListen(listenPort, AgentStatus.Nodeid, AgentStatus.AESKey)
	go PrepareForReOnlineNode()
	for {
		controlConnForLowerNode := <-node.NodeStuff.ControlConnForLowerNodeChan
		NewNodeMessage := <-node.NodeStuff.NewNodeMessageChan
		isAdmin := <-node.NodeStuff.IsAdmin
		if isAdmin {
			ConnToAdmin = controlConnForLowerNode
			AgentStatus.ReConnCome <- true
		} else {
			ProxyChan.ProxyChanToUpperNode <- NewNodeMessage
			if AgentStatus.NotLastOne == false {
				ProxyChan.ProxyChanToLowerNode = make(chan *common.PassToLowerNodeData)
				go HandleConnToLowerNode()
			}
			AgentStatus.NotLastOne = true
			lowerid := <-AgentStatus.WaitForIdAllocate
			go HandleConnFromLowerNode(controlConnForLowerNode, AgentStatus.Nodeid, lowerid)
		}
	}
}

//reverse mode下的普通节点
func SimpleNodeReversemodeInit(monitor, listenPort string) {
	AgentStatus.Nodeid = common.AdminId
	ConnToAdmin, AgentStatus.Nodeid = node.AcceptConnFromUpperNode(listenPort, AgentStatus.Nodeid, AgentStatus.AESKey)
	go common.SendHeartBeatControl(&ConnToAdmin, AgentStatus.Nodeid, AgentStatus.AESKey)
	go HandleSimpleNodeConn(&ConnToAdmin, AgentStatus.Nodeid)
	go node.StartNodeListen(listenPort, AgentStatus.Nodeid, AgentStatus.AESKey)
	go PrepareForReOnlineNode()
	for {
		controlConnForLowerNode := <-node.NodeStuff.ControlConnForLowerNodeChan
		NewNodeMessage := <-node.NodeStuff.NewNodeMessageChan
		<-node.NodeStuff.IsAdmin //被动模式启动的节点被连接一定是agent来连接，所以这里不需要判断是否是admin连接
		ProxyChan.ProxyChanToUpperNode <- NewNodeMessage
		if AgentStatus.NotLastOne == false {
			ProxyChan.ProxyChanToLowerNode = make(chan *common.PassToLowerNodeData)
			go HandleConnToLowerNode()
		}
		AgentStatus.NotLastOne = true
		lowerid := <-AgentStatus.WaitForIdAllocate
		go HandleConnFromLowerNode(controlConnForLowerNode, AgentStatus.Nodeid, lowerid)
	}
}

//reuseport下的startnode节点
func StartNodeReuseInit(reusehost, reuseport, localport string, method int) {
	AgentStatus.Nodeid = common.StartNodeId
	if method == 1 {
		ConnToAdmin, AgentStatus.Nodeid = node.AcceptConnFromUpperNodeReuse(reusehost, reuseport, AgentStatus.Nodeid, AgentStatus.AESKey)
	} else {
		err := node.SetPortReuseRules(localport, reuseport)
		if err != nil {
			fmt.Println("[*]Cannot set the iptable rules!")
			os.Exit(0)
		}
		ConnToAdmin, AgentStatus.Nodeid = node.AcceptConnFromUpperNodeIpTableReuse(reuseport, localport, AgentStatus.Nodeid, AgentStatus.AESKey)
	}
	go common.SendHeartBeatControl(&ConnToAdmin, AgentStatus.Nodeid, AgentStatus.AESKey)
	go HandleStartNodeConn(&ConnToAdmin, "", "", "", true, AgentStatus.Nodeid)
	if method == 1 {
		go node.StartNodeListenReuse(reusehost, reuseport, AgentStatus.Nodeid, AgentStatus.AESKey)
	} else {
		go node.StartNodeListenIpTableReuse(reuseport, localport, AgentStatus.Nodeid, AgentStatus.AESKey)
	}
	go PrepareForReOnlineNode()
	for {
		controlConnForLowerNode := <-node.NodeStuff.ControlConnForLowerNodeChan
		NewNodeMessage := <-node.NodeStuff.NewNodeMessageChan
		isAdmin := <-node.NodeStuff.IsAdmin
		if isAdmin {
			ConnToAdmin = controlConnForLowerNode
			AgentStatus.ReConnCome <- true
		} else {
			ProxyChan.ProxyChanToUpperNode <- NewNodeMessage
			if AgentStatus.NotLastOne == false {
				ProxyChan.ProxyChanToLowerNode = make(chan *common.PassToLowerNodeData)
				go HandleConnToLowerNode()
			}
			AgentStatus.NotLastOne = true
			lowerid := <-AgentStatus.WaitForIdAllocate
			go HandleConnFromLowerNode(controlConnForLowerNode, AgentStatus.Nodeid, lowerid)
		}
	}
}

//reuseport下的普通节点
func SimpleNodeReuseInit(reusehost, reuseport, localport string, method int) {
	AgentStatus.Nodeid = common.AdminId
	if method == 1 {
		ConnToAdmin, AgentStatus.Nodeid = node.AcceptConnFromUpperNodeReuse(reusehost, reuseport, AgentStatus.Nodeid, AgentStatus.AESKey)
	} else {
		err := node.SetPortReuseRules(localport, reuseport)
		if err != nil {
			fmt.Println("[*]Cannot set the iptable rules!")
			os.Exit(0)
		}
		ConnToAdmin, AgentStatus.Nodeid = node.AcceptConnFromUpperNodeIpTableReuse(reuseport, localport, AgentStatus.Nodeid, AgentStatus.AESKey)
	}
	go common.SendHeartBeatControl(&ConnToAdmin, AgentStatus.Nodeid, AgentStatus.AESKey)
	go HandleSimpleNodeConn(&ConnToAdmin, AgentStatus.Nodeid)
	if method == 1 {
		go node.StartNodeListenReuse(reusehost, reuseport, AgentStatus.Nodeid, AgentStatus.AESKey)
	} else {
		go node.StartNodeListenIpTableReuse(reuseport, localport, AgentStatus.Nodeid, AgentStatus.AESKey)
	}
	go PrepareForReOnlineNode()
	for {
		controlConnForLowerNode := <-node.NodeStuff.ControlConnForLowerNodeChan
		NewNodeMessage := <-node.NodeStuff.NewNodeMessageChan
		<-node.NodeStuff.IsAdmin
		ProxyChan.ProxyChanToUpperNode <- NewNodeMessage
		if AgentStatus.NotLastOne == false {
			ProxyChan.ProxyChanToLowerNode = make(chan *common.PassToLowerNodeData)
			go HandleConnToLowerNode()
		}
		AgentStatus.NotLastOne = true
		lowerid := <-AgentStatus.WaitForIdAllocate
		go HandleConnFromLowerNode(controlConnForLowerNode, AgentStatus.Nodeid, lowerid)
	}
}

//初始化代码结束

//startnode启动代码开始

//启动startnode
func HandleStartNodeConn(connToAdmin *net.Conn, monitor, listenPort, reConn string, passive bool, NODEID string) {
	go HandleConnFromAdmin(connToAdmin, monitor, listenPort, reConn, passive, NODEID)
	go HandleConnToAdmin(connToAdmin)
}

//管理startnode发往admin的数据
func HandleConnToAdmin(connToAdmin *net.Conn) {
	for {
		proxyData := <-ProxyChan.ProxyChanToUpperNode
		_, err := (*connToAdmin).Write(proxyData)
		if err != nil {
			continue
		}
	}
}

//看函数名猜功能.jpg XD
func HandleConnFromAdmin(connToAdmin *net.Conn, monitor, listenPort, reConn string, passive bool, NODEID string) {
	var (
		CannotRead = make(chan bool, 1)
		GetName    = make(chan bool, 1)
		stdin      io.Writer
		stdout     io.Reader
	)
	for {
		AdminData, err := common.ExtractPayload(*connToAdmin, AgentStatus.AESKey, NODEID, false)
		if err != nil {
			AdminOffline(reConn, monitor, listenPort, passive)
			continue
		}
		if AdminData.NodeId == NODEID {
			switch AdminData.Type {
			case "DATA":
				switch AdminData.Command {
				case "SOCKSDATA":
					SocksDataChanMap.RLock()
					if _, ok := SocksDataChanMap.Payload[AdminData.Clientid]; ok {
						SocksDataChanMap.Payload[AdminData.Clientid] <- AdminData.Info
						SocksDataChanMap.RUnlock()
					} else {
						SocksDataChanMap.RUnlock()
						SocksDataChanMap.Lock()
						SocksDataChanMap.Payload[AdminData.Clientid] = make(chan string, 10)
						go HanleClientSocksConn(SocksDataChanMap.Payload[AdminData.Clientid], SocksInfo.SocksUsername, SocksInfo.SocksPass, AdminData.Clientid, NODEID)
						SocksDataChanMap.Payload[AdminData.Clientid] <- AdminData.Info
						SocksDataChanMap.Unlock()
					}
				case "FILEDATA": //接收文件内容
					slicenum, _ := strconv.Atoi(AdminData.FileSliceNum)
					FileDataMap.Lock()
					FileDataMap.Payload[slicenum] = AdminData.Info
					FileDataMap.Unlock()
				case "FORWARD":
					TryForward(AdminData.Info, AdminData.Clientid)
				case "FORWARDDATA":
					ForwardConnMap.RLock()
					if _, ok := ForwardConnMap.Payload[AdminData.Clientid]; ok {
						PortFowardMap.Lock()
						if _, ok := PortFowardMap.Payload[AdminData.Clientid]; ok {
							PortFowardMap.Payload[AdminData.Clientid] <- AdminData.Info
						} else {
							PortFowardMap.Payload[AdminData.Clientid] = make(chan string, 10)
							go HandleForward(PortFowardMap.Payload[AdminData.Clientid], AdminData.Clientid)
							PortFowardMap.Payload[AdminData.Clientid] <- AdminData.Info
						}
						PortFowardMap.Unlock()
					}
					ForwardConnMap.RUnlock()
				case "FORWARDFIN":
					ForwardConnMap.Lock()
					if _, ok := ForwardConnMap.Payload[AdminData.Clientid]; ok {
						ForwardConnMap.Payload[AdminData.Clientid].Close()
						delete(ForwardConnMap.Payload, AdminData.Clientid)
					}
					ForwardConnMap.Unlock()
					PortFowardMap.Lock()
					if _, ok := PortFowardMap.Payload[AdminData.Clientid]; ok {
						if !common.IsClosed(PortFowardMap.Payload[AdminData.Clientid]) {
							if !common.IsClosed(PortFowardMap.Payload[AdminData.Clientid]) {
								close(PortFowardMap.Payload[AdminData.Clientid])
							}
							delete(PortFowardMap.Payload, AdminData.Clientid)
						}
					}
					PortFowardMap.Unlock()
				case "REFLECTDATARESP":
					ReflectConnMap.Lock()
					ReflectConnMap.Payload[AdminData.Clientid].Write([]byte(AdminData.Info))
					ReflectConnMap.Unlock()
				case "REFLECTTIMEOUT":
					fallthrough
				case "REFLECTOFFLINE":
					ReflectConnMap.Lock()
					if _, ok := ReflectConnMap.Payload[AdminData.Clientid]; ok {
						ReflectConnMap.Payload[AdminData.Clientid].Close()
						delete(ReflectConnMap.Payload, AdminData.Clientid)
					}
					ReflectConnMap.Unlock()
				case "FINOK":
					SocksDataChanMap.Lock() //性能损失？
					if _, ok := SocksDataChanMap.Payload[AdminData.Clientid]; ok {
						if !common.IsClosed(SocksDataChanMap.Payload[AdminData.Clientid]) {
							close(SocksDataChanMap.Payload[AdminData.Clientid])
						}
						delete(SocksDataChanMap.Payload, AdminData.Clientid)
					}
					SocksDataChanMap.Unlock()
				case "FIN":
					CurrentConn.Lock()
					if _, ok := CurrentConn.Payload[AdminData.Clientid]; ok {
						CurrentConn.Payload[AdminData.Clientid].Close()
						delete(CurrentConn.Payload, AdminData.Clientid)
					}
					CurrentConn.Unlock()
					SocksDataChanMap.Lock()
					if _, ok := SocksDataChanMap.Payload[AdminData.Clientid]; ok {
						if !common.IsClosed(SocksDataChanMap.Payload[AdminData.Clientid]) {
							close(SocksDataChanMap.Payload[AdminData.Clientid])
						}
						delete(SocksDataChanMap.Payload, AdminData.Clientid)
					}
					SocksDataChanMap.Unlock()
				case "HEARTBEAT":
					hbdatapack, _ := common.ConstructPayload(common.AdminId, "", "COMMAND", "KEEPALIVE", " ", " ", 0, NODEID, AgentStatus.AESKey, false)
					ProxyChan.ProxyChanToUpperNode <- hbdatapack
				}
			case "COMMAND":
				switch AdminData.Command {
				case "SHELL":
					switch AdminData.Info {
					case "":
						stdout, stdin = CreatInteractiveShell()
						go func() {
							StartShell("", stdin, stdout, NODEID)
						}()
					case "exit\n":
						fallthrough
					default:
						go func() {
							StartShell(AdminData.Info, stdin, stdout, NODEID)
						}()
					}
				case "SOCKS":
					socksinfo := strings.Split(AdminData.Info, ":::")
					SocksInfo.SocksUsername = socksinfo[1]
					SocksInfo.SocksPass = socksinfo[2]
					StartSocks()
				case "SOCKSOFF":
				case "SSH":
					err := StartSSH(AdminData.Info, NODEID)
					if err == nil {
						go ReadCommand()
					} else {
						break
					}
				case "SSHCOMMAND":
					go WriteCommand(AdminData.Info)
				case "SSHTUNNEL":
					err := SshTunnelNextNode(AdminData.Info, NODEID)
					if err != nil {
						fmt.Println("[*]", err)
						break
					}
				case "CONNECT":
					var status bool = false
					command := strings.Split(AdminData.Info, ":::")
					addr := command[0]
					choice := command[1]
					if choice == "1" { //连接的节点是否是在reuseport？
						status = node.ConnectNextNodeReuse(addr, NODEID, AgentStatus.AESKey)
					} else {
						status = node.ConnectNextNode(addr, NODEID, AgentStatus.AESKey)
					}
					if !status {
						message, _ := common.ConstructPayload(common.AdminId, "", "COMMAND", "NODECONNECTFAIL", " ", "", 0, NODEID, AgentStatus.AESKey, false)
						ProxyChan.ProxyChanToUpperNode <- message
					}
				case "FILENAME":
					var err error
					UploadFile, err := os.Create(AdminData.Info)
					if err != nil {
						respComm, _ := common.ConstructPayload(common.AdminId, "", "COMMAND", "CREATEFAIL", " ", " ", 0, NODEID, AgentStatus.AESKey, false)
						ProxyChan.ProxyChanToUpperNode <- respComm
					} else {
						respComm, _ := common.ConstructPayload(common.AdminId, "", "COMMAND", "NAMECONFIRM", " ", " ", 0, NODEID, AgentStatus.AESKey, false)
						ProxyChan.ProxyChanToUpperNode <- respComm
						go common.ReceiveFile("", connToAdmin, FileDataMap, CannotRead, UploadFile, AgentStatus.AESKey, false, NODEID)
					}
				case "FILESIZE":
					filesize, _ := strconv.ParseInt(AdminData.Info, 10, 64)
					common.File.FileSize = filesize
					respComm, _ := common.ConstructPayload(common.AdminId, "", "COMMAND", "FILESIZECONFIRM", " ", " ", 0, NODEID, AgentStatus.AESKey, false)
					ProxyChan.ProxyChanToUpperNode <- respComm
					common.File.ReceiveFileSize <- true
				case "FILESLICENUM":
					common.File.TotalSilceNum, _ = strconv.Atoi(AdminData.Info)
					respComm, _ := common.ConstructPayload(common.AdminId, "", "COMMAND", "FILESLICENUMCONFIRM", " ", " ", 0, NODEID, AgentStatus.AESKey, false)
					ProxyChan.ProxyChanToUpperNode <- respComm
					common.File.ReceiveFileSliceNum <- true
				case "FILESLICENUMCONFIRM":
					common.File.TotalConfirm <- true
				case "FILESIZECONFIRM":
					common.File.TotalConfirm <- true
				case "DOWNLOADFILE":
					go common.UploadFile("", AdminData.Info, connToAdmin, common.AdminId, GetName, AgentStatus.AESKey, NODEID, false)
				case "NAMECONFIRM":
					GetName <- true
				case "CREATEFAIL":
					GetName <- false
				case "CANNOTREAD":
					CannotRead <- true
					common.File.ReceiveFileSliceNum <- false
					os.Remove(AdminData.Info) //删除空文件
				case "FORWARDTEST":
					go TestForward(AdminData.Info)
				case "REFLECTTEST":
					go TestReflect(AdminData.Info)
				case "REFLECTNUM":
					ReflectStatus.ReflectNum <- AdminData.Clientid
				case "STOPREFLECT":
					ReflectConnMap.Lock()
					for key, conn := range ReflectConnMap.Payload {
						conn.Close()
						delete(ForwardConnMap.Payload, key)
					}
					ReflectConnMap.Unlock()

					for _, listener := range CurrentPortReflectListener {
						listener.Close()
					}
				case "LISTEN":
					err := TestListen(AdminData.Info)
					if err != nil {
						respComm, _ := common.ConstructPayload(common.AdminId, "", "COMMAND", "LISTENRESP", " ", "FAILED", 0, NODEID, AgentStatus.AESKey, false)
						ProxyChan.ProxyChanToUpperNode <- respComm
					} else {
						respComm, _ := common.ConstructPayload(common.AdminId, "", "COMMAND", "LISTENRESP", " ", "SUCCESS", 0, NODEID, AgentStatus.AESKey, false)
						ProxyChan.ProxyChanToUpperNode <- respComm
						go node.StartNodeListen(AdminData.Info, NODEID, AgentStatus.AESKey)
					}
				case "KEEPALIVE":
				default:
					continue
				}
			}
		} else {
			// 检查是否是admin发来的，分配给自己子节点的ID命令，是的话将admin分配的序号记录
			if AdminData.Route == "" && AdminData.Command == "ID" {
				AgentStatus.WaitForIdAllocate <- AdminData.NodeId //将此节点序号递交，以便启动HandleConnFromLowerNode函数
				node.NodeInfo.LowerNode.Lock()
				node.NodeInfo.LowerNode.Payload[AdminData.NodeId] = node.NodeInfo.LowerNode.Payload[common.AdminId]
				node.NodeInfo.LowerNode.Unlock()
			}
			routeid := ChangeRoute(AdminData) //更改路由并返回下一个路由点
			proxyData, _ := common.ConstructPayload(AdminData.NodeId, AdminData.Route, AdminData.Type, AdminData.Command, AdminData.FileSliceNum, AdminData.Info, AdminData.Clientid, AdminData.CurrentId, AgentStatus.AESKey, true)
			passToLowerData := common.NewPassToLowerNodeData()
			if routeid == "" { //当返回的路由点为0，说明就是自己的子节点
				passToLowerData.Route = AdminData.NodeId
			} else { //不是0，说明不是自己的子节点，还需要一定轮数的递送
				passToLowerData.Route = routeid
			}
			passToLowerData.Data = proxyData //封装结构体，交给HandleConnToLowerNode处理
			ProxyChan.ProxyChanToLowerNode <- passToLowerData
		}
	}
}

//startnode启动代码结束

//管理下行节点代码开始

//管理发往下级节点的信道
func HandleConnToLowerNode() {
	for {
		proxyData := <-ProxyChan.ProxyChanToLowerNode
		node.NodeInfo.LowerNode.Lock()
		if _, ok := node.NodeInfo.LowerNode.Payload[proxyData.Route]; ok { //检查此节点是否存活，防止admin误操作在已掉线的节点输入命令导致节点panic
			node.NodeInfo.LowerNode.Payload[proxyData.Route].Write(proxyData.Data)
		}
		node.NodeInfo.LowerNode.Unlock()
	}
}

//看到那个from了么
func HandleConnFromLowerNode(connForLowerNode net.Conn, currentid, lowerid string) {
	for {
		command, err := common.ExtractPayload(connForLowerNode, AgentStatus.AESKey, currentid, false)
		if err != nil {
			connForLowerNode.Close()
			node.NodeInfo.LowerNode.Lock()
			delete(node.NodeInfo.LowerNode.Payload, lowerid) //下级节点掉线，立即将此节点从自己的子节点列表删除
			node.NodeInfo.LowerNode.Unlock()
			offlineMess, _ := common.ConstructPayload(common.AdminId, "", "COMMAND", "AGENTOFFLINE", " ", lowerid, 0, currentid, AgentStatus.AESKey, false) //通知admin下级节点已经下线
			ProxyChan.ProxyChanToUpperNode <- offlineMess
			return
		}
		switch command.Type {
		case "COMMAND":
			switch command.Command {
			case "RECONNID":
				if _, ok := node.NodeInfo.LowerNode.Payload[command.CurrentId]; ok {
					info := fmt.Sprintf("%s:::%s", currentid, connForLowerNode.RemoteAddr().String())
					proxyCommand, _ := common.ConstructPayload(common.AdminId, "", "COMMAND", command.Command, " ", info, 0, command.CurrentId, AgentStatus.AESKey, false)
					ProxyChan.ProxyChanToUpperNode <- proxyCommand
					continue
				} else {
					proxyCommand, _ := common.ConstructPayload(command.NodeId, command.Route, command.Type, command.Command, command.FileSliceNum, command.Info, command.Clientid, command.CurrentId, AgentStatus.AESKey, true)
					ProxyChan.ProxyChanToUpperNode <- proxyCommand
					continue
				}
			case "HEARTBEAT":
				hbcommpack, _ := common.ConstructPayload(command.CurrentId, "", "COMMAND", "KEEPALIVE", " ", " ", 0, currentid, AgentStatus.AESKey, false)
				passToLowerData := common.NewPassToLowerNodeData()
				passToLowerData.Data = hbcommpack
				passToLowerData.Route = command.CurrentId
				ProxyChan.ProxyChanToLowerNode <- passToLowerData
				continue
			default:
				proxyData, _ := common.ConstructPayload(command.NodeId, command.Route, command.Type, command.Command, command.FileSliceNum, command.Info, command.Clientid, command.CurrentId, AgentStatus.AESKey, true)
				ProxyChan.ProxyChanToUpperNode <- proxyData
			}
		case "DATA":
			proxyData, _ := common.ConstructPayload(command.NodeId, command.Route, command.Type, command.Command, command.FileSliceNum, command.Info, command.Clientid, command.CurrentId, AgentStatus.AESKey, true)
			ProxyChan.ProxyChanToUpperNode <- proxyData
		}
	}
}

//管理下行节点代码结束

//普通节点启动代码开始

//启动普通节点
func HandleSimpleNodeConn(connToUpperNode *net.Conn, NODEID string) {
	go HandleConnFromUpperNode(connToUpperNode, NODEID)
	go HandleConnToUpperNode(connToUpperNode)
}

// 处理发往上一级节点的控制信道
func HandleConnToUpperNode(connToUpperNode *net.Conn) {
	for {
		proxyData := <-ProxyChan.ProxyChanToUpperNode
		_, err := (*connToUpperNode).Write(proxyData)
		if err != nil {
			continue
		}
	}
}

//处理来自上一级节点的控制信道
func HandleConnFromUpperNode(connToUpperNode *net.Conn, NODEID string) {
	var (
		CannotRead = make(chan bool, 1)
		GetName    = make(chan bool, 1)
		stdin      io.Writer
		stdout     io.Reader
	)
	for {
		command, err := common.ExtractPayload(*connToUpperNode, AgentStatus.AESKey, NODEID, false)
		if err != nil {
			node.NodeStuff.Offline = true
			WaitingAdmin(NODEID) //上一级节点间网络连接断开后不掉线，等待上级节点重连回来
			continue
		}
		if command.NodeId == NODEID {
			switch command.Type {
			case "COMMAND":
				switch command.Command {
				case "SHELL":
					switch command.Info {
					case "":
						stdout, stdin = CreatInteractiveShell()
						go func() {
							StartShell("", stdin, stdout, NODEID)
						}()
					case "exit\n":
						fallthrough
					default:
						go func() {
							StartShell(command.Info, stdin, stdout, NODEID)
						}()
					}
				case "SOCKS":
					socksinfo := strings.Split(command.Info, ":::")
					SocksInfo.SocksUsername = socksinfo[1]
					SocksInfo.SocksPass = socksinfo[2]
					StartSocks()
				case "SOCKSOFF":
				case "SSH":
					err := StartSSH(command.Info, NODEID)
					if err == nil {
						go ReadCommand()
					} else {
						break
					}
				case "SSHCOMMAND":
					go WriteCommand(command.Info)
				case "SSHTUNNEL":
					err := SshTunnelNextNode(command.Info, NODEID)
					if err != nil {
						fmt.Println("[*]", err)
						break
					}
				case "CONNECT":
					status := node.ConnectNextNode(command.Info, NODEID, AgentStatus.AESKey)
					if !status {
						message, _ := common.ConstructPayload(common.AdminId, "", "COMMAND", "NODECONNECTFAIL", " ", "", 0, NODEID, AgentStatus.AESKey, false)
						ProxyChan.ProxyChanToUpperNode <- message
					}
				case "FILENAME":
					var err error
					UploadFile, err := os.Create(command.Info)
					if err != nil {
						respComm, _ := common.ConstructPayload(common.AdminId, "", "COMMAND", "CREATEFAIL", " ", " ", 0, NODEID, AgentStatus.AESKey, false)
						ProxyChan.ProxyChanToUpperNode <- respComm
					} else {
						respComm, _ := common.ConstructPayload(common.AdminId, "", "COMMAND", "NAMECONFIRM", " ", " ", 0, NODEID, AgentStatus.AESKey, false)
						ProxyChan.ProxyChanToUpperNode <- respComm
						go common.ReceiveFile("", connToUpperNode, FileDataMap, CannotRead, UploadFile, AgentStatus.AESKey, false, NODEID)
					}
				case "FILESIZE":
					common.File.FileSize, _ = strconv.ParseInt(command.Info, 10, 64)
					respComm, _ := common.ConstructPayload(common.AdminId, "", "COMMAND", "FILESIZECONFIRM", " ", " ", 0, NODEID, AgentStatus.AESKey, false)
					ProxyChan.ProxyChanToUpperNode <- respComm
					common.File.ReceiveFileSize <- true
				case "FILESLICENUM":
					common.File.TotalSilceNum, _ = strconv.Atoi(command.Info)
					respComm, _ := common.ConstructPayload(common.AdminId, "", "COMMAND", "FILESLICENUMCONFIRM", " ", " ", 0, NODEID, AgentStatus.AESKey, false)
					ProxyChan.ProxyChanToUpperNode <- respComm
					common.File.ReceiveFileSliceNum <- true
				case "FILESLICENUMCONFIRM":
					common.File.TotalConfirm <- true
				case "FILESIZECONFIRM":
					common.File.TotalConfirm <- true
				case "DOWNLOADFILE":
					go common.UploadFile("", command.Info, connToUpperNode, common.AdminId, GetName, AgentStatus.AESKey, NODEID, false)
				case "NAMECONFIRM":
					GetName <- true
				case "CREATEFAIL":
					GetName <- false
				case "CANNOTREAD":
					CannotRead <- true
					common.File.ReceiveFileSliceNum <- false
					os.Remove(command.Info)
				case "FORWARDTEST":
					go TestForward(command.Info)
				case "REFLECTTEST":
					go TestReflect(command.Info)
				case "REFLECTNUM":
					ReflectStatus.ReflectNum <- command.Clientid
				case "STOPREFLECT":
					ReflectConnMap.Lock()
					for key, conn := range ReflectConnMap.Payload {
						conn.Close()
						delete(ForwardConnMap.Payload, key)
					}
					ReflectConnMap.Unlock()

					for _, listener := range CurrentPortReflectListener {
						listener.Close()
					}
				case "RECONN":
					respCommand, _ := common.ConstructPayload(common.AdminId, "", "COMMAND", "RECONNID", " ", "", 0, NODEID, AgentStatus.AESKey, false)
					ProxyChan.ProxyChanToUpperNode <- respCommand
					if AgentStatus.NotLastOne {
						BroadCast("RECONN")
					}
				case "CLEAR":
					ClearAllConn()
					SocksDataChanMap = common.NewUint32ChanStrMap()
					if AgentStatus.NotLastOne {
						BroadCast("CLEAR")
					}
				case "LISTEN":
					err := TestListen(command.Info)
					if err != nil {
						respComm, _ := common.ConstructPayload(common.AdminId, "", "COMMAND", "LISTENRESP", " ", "FAILED", 0, NODEID, AgentStatus.AESKey, false)
						ProxyChan.ProxyChanToUpperNode <- respComm
					} else {
						respComm, _ := common.ConstructPayload(common.AdminId, "", "COMMAND", "LISTENRESP", " ", "SUCCESS", 0, NODEID, AgentStatus.AESKey, false)
						ProxyChan.ProxyChanToUpperNode <- respComm
						go node.StartNodeListen(command.Info, NODEID, AgentStatus.AESKey)
					}
				case "KEEPALIVE":
				default:
					continue
				}
			case "DATA":
				switch command.Command {
				case "SOCKSDATA":
					SocksDataChanMap.RLock()
					if _, ok := SocksDataChanMap.Payload[command.Clientid]; ok {
						SocksDataChanMap.Payload[command.Clientid] <- command.Info
						SocksDataChanMap.RUnlock()
					} else {
						SocksDataChanMap.RUnlock()
						SocksDataChanMap.Lock()
						SocksDataChanMap.Payload[command.Clientid] = make(chan string, 10)
						go HanleClientSocksConn(SocksDataChanMap.Payload[command.Clientid], SocksInfo.SocksUsername, SocksInfo.SocksPass, command.Clientid, NODEID)
						SocksDataChanMap.Payload[command.Clientid] <- command.Info
						SocksDataChanMap.Unlock()
					}
				case "FINOK":
					SocksDataChanMap.Lock()
					if _, ok := SocksDataChanMap.Payload[command.Clientid]; ok {
						if !common.IsClosed(SocksDataChanMap.Payload[command.Clientid]) {
							close(SocksDataChanMap.Payload[command.Clientid])
						}
						delete(SocksDataChanMap.Payload, command.Clientid)
					}
					SocksDataChanMap.Unlock()
				case "FILEDATA": //接收文件内容
					slicenum, _ := strconv.Atoi(command.FileSliceNum)
					FileDataMap.Lock()
					FileDataMap.Payload[slicenum] = command.Info
					FileDataMap.Unlock()
				case "FIN":
					CurrentConn.Lock()
					if _, ok := CurrentConn.Payload[command.Clientid]; ok {
						err := CurrentConn.Payload[command.Clientid].Close()
						if err != nil {
						}
						delete(CurrentConn.Payload, command.Clientid)
					}
					CurrentConn.Unlock()
					SocksDataChanMap.Lock()
					if _, ok := SocksDataChanMap.Payload[command.Clientid]; ok {
						if !common.IsClosed(SocksDataChanMap.Payload[command.Clientid]) {
							close(SocksDataChanMap.Payload[command.Clientid])
						}
						delete(SocksDataChanMap.Payload, command.Clientid)
					}
					SocksDataChanMap.Unlock()
				case "FORWARD": //连接指定需要映射的端口
					TryForward(command.Info, command.Clientid)
				case "FORWARDDATA":
					ForwardConnMap.RLock()
					if _, ok := ForwardConnMap.Payload[command.Clientid]; ok {
						PortFowardMap.Lock()
						if _, ok := PortFowardMap.Payload[command.Clientid]; ok {
							PortFowardMap.Payload[command.Clientid] <- command.Info
						} else {
							PortFowardMap.Payload[command.Clientid] = make(chan string, 10)
							go HandleForward(PortFowardMap.Payload[command.Clientid], command.Clientid)
							PortFowardMap.Payload[command.Clientid] <- command.Info
						}
						PortFowardMap.Unlock()
					}
					ForwardConnMap.RUnlock()
				case "FORWARDFIN":
					ForwardConnMap.Lock()
					if _, ok := ForwardConnMap.Payload[command.Clientid]; ok {
						ForwardConnMap.Payload[command.Clientid].Close()
						delete(ForwardConnMap.Payload, command.Clientid)
					}
					ForwardConnMap.Unlock()
					PortFowardMap.Lock()
					if _, ok := PortFowardMap.Payload[command.Clientid]; ok {
						if !common.IsClosed(PortFowardMap.Payload[command.Clientid]) {
							close(PortFowardMap.Payload[command.Clientid])
						}
					}
					PortFowardMap.Unlock()
				case "REFLECTDATARESP":
					ReflectConnMap.Lock()
					ReflectConnMap.Payload[command.Clientid].Write([]byte(command.Info))
					ReflectConnMap.Unlock()
				case "REFLECTTIMEOUT":
					fallthrough
				case "REFLECTOFFLINE":
					ReflectConnMap.Lock()
					if _, ok := ReflectConnMap.Payload[command.Clientid]; ok {
						ReflectConnMap.Payload[command.Clientid].Close()
						delete(ReflectConnMap.Payload, command.Clientid)
					}
					ReflectConnMap.Unlock()
				default:
					continue
				}
			}
		} else {
			if command.Route == "" && command.Command == "ID" {
				AgentStatus.WaitForIdAllocate <- command.NodeId
				node.NodeInfo.LowerNode.Lock()
				node.NodeInfo.LowerNode.Payload[command.NodeId] = node.NodeInfo.LowerNode.Payload[common.AdminId]
				node.NodeInfo.LowerNode.Unlock()
			}
			routeid := ChangeRoute(command)
			proxyData, _ := common.ConstructPayload(command.NodeId, command.Route, command.Type, command.Command, command.FileSliceNum, command.Info, command.Clientid, command.CurrentId, AgentStatus.AESKey, true)
			passToLowerData := common.NewPassToLowerNodeData()
			if routeid == "" {
				passToLowerData.Route = command.NodeId
			} else {
				passToLowerData.Route = routeid
			}
			passToLowerData.Data = proxyData
			ProxyChan.ProxyChanToLowerNode <- passToLowerData
		}
	}
}

//普通节点启动代码结束

//agent主体代码结束
