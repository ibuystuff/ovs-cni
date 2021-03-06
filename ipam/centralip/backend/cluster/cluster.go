// Copyright (c) 2017
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package cluster

import (
	"context"
	"fmt"
	"github.com/John-Lin/ovs-cni/ipam/centralip/backend/utils"
	"github.com/containernetworking/plugins/pkg/ip"
	"github.com/coreos/etcd/clientv3"
	"net"
	"time"
)

type NodeIPM struct {
	cli     *clientv3.Client
	podname string
	subnet  *net.IPNet
	config  *utils.IPMConfig
}

const clusterPrefix string = utils.ETCDPrefix + "cluster/"

func New(podName string, config *utils.IPMConfig) (*NodeIPM, error) {
	node := &NodeIPM{}
	node.config = config
	var err error

	node.podname = podName
	err = node.connect(config.ETCDURL)
	if err != nil {
		return nil, err
	}

	_, node.subnet, err = net.ParseCIDR(config.Network)
	if err != nil {
		return nil, err
	}
	return node, nil
}

/*
	ETCD Related
*/
func (node *NodeIPM) connect(etcdUrl string) error {
	cli, err := clientv3.New(clientv3.Config{
		Endpoints:   []string{etcdUrl},
		DialTimeout: 5 * time.Second,
	})

	node.cli = cli
	return err
}

func (node *NodeIPM) deleteKey(prefix string) error {
	_, err := node.cli.Delete(context.TODO(), prefix)
	return err
}
func (node *NodeIPM) putValue(prefix, value string) error {
	_, err := node.cli.Put(context.TODO(), prefix, value)
	return err
}

func (node *NodeIPM) getKeyValuesWithPrefix(key string) (map[string]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	resp, err := node.cli.Get(ctx, key, clientv3.WithPrefix())
	cancel()
	if err != nil {
		return nil, fmt.Errorf("Fetch etcd prefix error:%v", err)
	}

	results := make(map[string]string)
	for _, ev := range resp.Kvs {
		results[string(ev.Key)] = string(ev.Value)
	}

	return results, nil
}

func (node *NodeIPM) GetGateway() (string, error) {
	return "", nil
}

func (node *NodeIPM) GetAvailableIP() (string, *net.IPNet, error) {
	ipnet := &net.IPNet{}
	if node.subnet == nil {
		return "", ipnet, fmt.Errorf("You should init IPM first")
	}

	usedIPPrefix := clusterPrefix + "used/"
	ipUsedToPod, err := node.getKeyValuesWithPrefix(usedIPPrefix)
	if err != nil {
		return "", ipnet, err
	}

	len, _ := node.subnet.Mask.Size()
	ipRange := utils.PowTwo(32 - (len))
	//Since the first IP is gateway, we should skip it
	tmpIP := ip.NextIP(utils.GetNextIP(node.subnet))

	var availableIP string
	for i := 1; i < int(ipRange); i++ {
		//check.
		if _, ok := ipUsedToPod[usedIPPrefix+tmpIP.String()]; !ok {
			availableIP = tmpIP.String()
			node.putValue(usedIPPrefix+tmpIP.String(), node.podname)
			break
		}
		tmpIP = ip.NextIP(tmpIP)
	}

	//We need to generate a net.IPnet object which contains the IP and Mask.
	//We use ParseCIDR to create the net.IPnet object and assign IP back to it.
	cidr := fmt.Sprintf("%s/%d", availableIP, len)
	var ip net.IP
	ip, ipnet, err = net.ParseCIDR(cidr)
	if err != nil {
		return "", ipnet, err
	}

	ipnet.IP = ip
	return availableIP, ipnet, nil
}

func (node *NodeIPM) Delete() error {
	//get all used ip address and try to matches it id.
	usedIPPrefix := clusterPrefix + "used/"
	ipUsedToPod, err := node.getKeyValuesWithPrefix(usedIPPrefix)
	if err != nil {
		return err
	}

	for k, v := range ipUsedToPod {
		fmt.Println(k, v)
		if v == node.podname {
			err := node.deleteKey(k)
			return err
		}
	}
	return fmt.Errorf("There aren't any infomation about pod %s", node.podname)
}
