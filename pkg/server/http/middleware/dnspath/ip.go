package dnspath

import (
	"fmt"
	"net"
	"sort"
	"strconv"
)

type IPHolder struct {
	ip map[int]string
}

func NewIPHolder() *IPHolder {
	return &IPHolder{
		ip: make(map[int]string),
	}
}

func (h *IPHolder) GetStr(i string) string {
	number, _ := strconv.ParseInt(i, 10, 64)

	return h.Get(int(number))
}

func (h *IPHolder) Dump() string {
	return fmt.Sprintf("%v", h.ip)
}

func (h *IPHolder) Get(i int) string {
	if h.ip == nil {
		return ""
	}

	return h.ip[i]
}

func (h *IPHolder) Set(ips []net.IP) {
	// find our ip
	ipMap := make(map[string]int, len(h.ip))
	remainingNumbers := NewNumber(len(h.ip))
	ipDNSMap := make(map[string]struct{}, len(ips))

	highNumber := 0

	for number, ip := range h.ip {
		ipMap[ip] = number

		remainingNumbers.Set(number)

		if number > highNumber {
			highNumber = number
		}
	}

	newIPs := make([]string, 0, len(ips))
	for _, ip := range ips {
		ipStr := ip.String()
		ipDNSMap[ipStr] = struct{}{}

		if _, ok := ipMap[ipStr]; ok {
			// remove in all numbers
			remainingNumbers.Delete(ipMap[ipStr])

			continue
		}

		newIPs = append(newIPs, ipStr)
	}

	remainingNumbers.Order()

	// add new ips on the remaning numbers
	for _, ip := range newIPs {
		number := remainingNumbers.Pop()
		if number == 0 {
			// no more numbers, add new one
			highNumber++
			number = highNumber
		}

		h.ip[number] = ip
		ipMap[ip] = number
	}

	for number, ip := range h.ip {
		if _, ok := ipDNSMap[ip]; !ok {
			delete(h.ip, number)
		}
	}

	ipSlice := getOrderedSlice(h.ip)
	h.ip = make(map[int]string, len(ipSlice))
	for i, ip := range ipSlice {
		h.ip[i+1] = ip
	}
}

func getOrderedSlice(m map[int]string) []string {
	keys := make([]int, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Ints(keys)

	slice := make([]string, 0, len(m))
	for _, k := range keys {
		slice = append(slice, m[k])
	}

	return slice
}
