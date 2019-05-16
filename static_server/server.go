package main

import (
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
)

func getMyInterfaceAddr() ([]net.IP, error) {

	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	addresses := []net.IP{}
	for _, iface := range ifaces {

		if iface.Flags&net.FlagUp == 0 {
			continue // interface down
		}
		if iface.Flags&net.FlagLoopback != 0 {
			continue // loopback interface
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}

		for _, addr := range addrs {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			if ip == nil || ip.IsLoopback() {
				continue
			}
			ip = ip.To4()
			if ip == nil {
				continue // not an ipv4 address
			}
			addresses = append(addresses, ip)
		}
	}
	if len(addresses) == 0 {
		return nil, fmt.Errorf("no address Found, net.InterfaceAddrs: %v", addresses)
	}
	//only need first
	return addresses, nil
}

func main() {

	var port = flag.Int("port", 8080, "port for server to use")
	var path = flag.String("path", "./html", "serve files from this directory")
	var verb = flag.Bool("verbose", false, "show port and directory on stating up")
	flag.Parse()

	stat, err := os.Stat(*path)
	if err != nil {
		if os.IsNotExist(err) {
			println("Directory Does not exists.")
		} else {
			fmt.Print("os.Stat(): error for folder name ", *path)
			fmt.Println(" and error is : ", err.Error())
		}
		os.Exit(1)
	}

	if !stat.IsDir() {
		fmt.Printf("Given path, %s,  is not a directory!\n", *path)
		os.Exit(1)
	}

	http.Handle("/", http.FileServer(http.Dir(*path)))
	if *verb == true {
		fmt.Printf("starting server on port %d\n", *port)
		fmt.Printf("serving files from %s\n", *path)

		ips, err := getMyInterfaceAddr()
		if err != nil {
			fmt.Println("can't get an ip address...")
			os.Exit(1)
		}
		fmt.Println("view files at one of these addresses:")
		for _, ip := range ips {
			fmt.Printf("\thttp://%s:%d/\n", ip.String(), *port)
		}
	}
	http.ListenAndServe(fmt.Sprintf(":%d", *port), nil)
}
