package IP

import (
	"bytes"
	"fmt"
	"kscan/lib/misc"
	"regexp"
	"strconv"
	"strings"
)

/*
	根据网络网段，获取该段所有IP
*/

var regxIsIP = regexp.MustCompile("^\\d{1,3}\\.\\d{1,3}\\.\\d{1,3}\\.\\d{1,3}$")
var regxIsIPMask = regexp.MustCompile("^(\\d{1,3}\\.\\d{1,3}\\.\\d{1,3}\\.\\d{1,3})/(\\d{1,2})$")
var regxIsIPRange = regexp.MustCompile("^(\\d{1,3}\\.\\d{1,3}\\.\\d{1,3}\\.\\d{1,3})-(\\d{1,3}\\.\\d{1,3}\\.\\d{1,3}\\.\\d{1,3})$")
var regxIsIPRange2 = regexp.MustCompile("^(\\d{1,3}-\\d{1,3}|\\d{1,3})\\.(\\d{1,3}-\\d{1,3}|\\d{1,3})\\.(\\d{1,3}-\\d{1,3}|\\d{1,3})\\.(\\d{1,3}-\\d{1,3}|\\d{1,3})$")

var regxPrivateIPArr = []*regexp.Regexp{
	regexp.MustCompile("^10\\.\\d{1,3}\\.\\d{1,3}\\.\\d{1,3}$"),
	regexp.MustCompile("^172\\.(?:[123]\\d)\\.\\d{1,3}\\.\\d{1,3}$"),
	regexp.MustCompile("^192\\.168\\.\\d{1,3}\\.\\d{1,3}$"),
}

func FormatCheck(ipExpr string) bool {
	if regxIsIP.MatchString(ipExpr) {
		return AddrCheck(ipExpr)
	}
	if regxIsIPMask.MatchString(ipExpr) {
		ip := regxIsIPMask.FindStringSubmatch(ipExpr)[1]
		mask := regxIsIPMask.FindStringSubmatch(ipExpr)[2]
		if AddrCheck(ip) == false {
			return false
		}
		if maskCheck(mask) == false {
			return false
		}
		return true
	}
	if regxIsIPRange.MatchString(ipExpr) {
		first := regxIsIPRange.FindStringSubmatch(ipExpr)[1]
		last := regxIsIPRange.FindStringSubmatch(ipExpr)[2]
		if AddrCheck(first) == false {
			return false
		}
		if AddrCheck(last) == false {
			return false
		}
		firstInt := addrStrToInt(first)
		lastInt := addrStrToInt(last)
		if firstInt > lastInt {
			return false
		}
		return true
	} else if regxIsIPRange2.MatchString(ipExpr) {
		submatch := regxIsIPRange2.FindStringSubmatch(ipExpr)
		if len(submatch) != 5 {
			return false
		}
		for i := 1; i < 5; i++ {
			if strings.Contains(submatch[i], "-") {
				subIpInt := strings.Split(submatch[i], "-")
				if len(subIpInt) != 2 {
					return false
				}
				if misc.Str2Int(subIpInt[0]) > misc.Str2Int(subIpInt[1]) {
					return false
				}
			}
		}
		return true
	}
	return false
}

func IsIP(s string) bool {
	if regxIsIP.MatchString(s) {
		return AddrCheck(s)
	}
	return false
}

func IsInSameSegment(ips []string) bool {
	if len(ips) == 0 {
		return false
	}
	networkSegment := ExprToList(ips[0] + "/24")
	if len(networkSegment) == 0 {
		return false
	}
	start := StringIpToInt(networkSegment[0])
	end := StringIpToInt(networkSegment[len(networkSegment)-1])
	for _, ip := range ips {
		ipInt := StringIpToInt(ip)
		if ipInt > end || ipInt < start {
			return false
		}
	}
	return true
}

func GetGatewayList(ip string, t string) []string {
	var gatewayArr []string
	if FormatCheck(ip) == false {
		return gatewayArr
	}
	strArr := strings.Split(ip, ".")
	if t == "b" {
		for i := 0; i < 255; i++ {
			gatewayArr = append(gatewayArr, fmt.Sprintf("%s.%s.%d.1", strArr[0], strArr[1], i))
			gatewayArr = append(gatewayArr, fmt.Sprintf("%s.%s.%d.255", strArr[0], strArr[1], i))
		}
	}
	if t == "a" {
		for i := 0; i < 255; i++ {
			for j := 0; j < 255; j++ {
				gatewayArr = append(gatewayArr, fmt.Sprintf("%s.%d.%d.1", strArr[0], i, j))
				gatewayArr = append(gatewayArr, fmt.Sprintf("%s.%d.%d.255", strArr[0], i, j))
			}
		}
	}
	if t == "s" {
		for i := 0; i < 255; i++ {
			gatewayArr = append(gatewayArr, fmt.Sprintf("%d.%d.%d.1", i, i, i))
			gatewayArr = append(gatewayArr, fmt.Sprintf("%d.%d.%d.255", i, i, i))
		}
	}
	return gatewayArr
}

//func IsPrivateIPAddr(ip string) bool {
//	for _, regxPrivateIP := range regxPrivateIPArr {
//		if regxPrivateIP.MatchString(ip) {
//			return true
//		}
//	}
//	return false
//}

func ExprToList(ipExpr string) []string {
	var r []string
	if regxIsIP.MatchString(ipExpr) {
		return append(r, ipExpr)
	}
	if regxIsIPMask.MatchString(ipExpr) {
		ip := regxIsIPMask.FindStringSubmatch(ipExpr)[1]
		mask := regxIsIPMask.FindStringSubmatch(ipExpr)[2]
		maskInt, _ := strconv.Atoi(mask)
		ipInt := addrStrToInt(ip)
		maskhead := uint32(0xFFFFFFFF)
		for i := 1; i <= 32-maskInt; i++ {
			maskhead = maskhead << 1
		}
		masktail := uint32(0xFFFFFFFF)
		for i := 1; i <= maskInt; i++ {
			masktail = masktail >> 1
		}
		ipStart := uint32(ipInt) & maskhead
		ipEnd := uint32(ipInt) | masktail
		return RangeToList(ipStart, ipEnd)
	}
	if regxIsIPRange.MatchString(ipExpr) {
		start := regxIsIPRange.FindStringSubmatch(ipExpr)[1]
		end := regxIsIPRange.FindStringSubmatch(ipExpr)[2]
		startInt := addrStrToInt(start)
		endInt := addrStrToInt(end)
		return RangeToList(uint32(startInt), uint32(endInt))
	} else if regxIsIPRange2.MatchString(ipExpr) {
		submatch := regxIsIPRange2.FindStringSubmatch(ipExpr)
		var subIps [][]string
		for i := 1; i < 5; i++ {
			if strings.Contains(submatch[i], "-") {
				subIpInt := strings.Split(submatch[i], "-")
				subStart, _ := strconv.Atoi(subIpInt[0])
				subEnd, _ := strconv.Atoi(subIpInt[1])
				subIps = append(subIps, misc.IntArr2StrArr(misc.Xrange(subStart, subEnd)))
			} else {
				subIps = append(subIps, []string{submatch[i]})
			}
		}
		for _, ip0 := range subIps[0] {
			for _, ip1 := range subIps[1] {
				for _, ip2 := range subIps[2] {
					for _, ip3 := range subIps[3] {
						ipStr := fmt.Sprintf("%s.%s.%s.%s", ip0, ip1, ip2, ip3)
						r = append(r, ipStr)
					}
				}
			}
		}
		r = misc.RemoveDuplicateElement(r)
	}
	return r
}

func RangeToList(start uint32, end uint32) (result []string) {
	for i := start; i <= end; i++ {
		result = append(result, addrIntToStr(int(i)))
	}
	return result
}

func AddrCheck(ip string) bool {
	sArr := strings.Split(ip, ".")
	if len(sArr) != 4 {
		return false
	}
	for _, s := range sArr {
		i, err := strconv.Atoi(s)
		if err != nil {
			return false
		}
		if i > 255 || i < 0 {
			return false
		}
	}
	return true
}

func addrStrToInt(ipStr string) int {
	ipArr := strings.Split(ipStr, ".")
	var ipInt int
	var pos uint = 24
	for _, ipSeg := range ipArr {
		tempInt, _ := strconv.Atoi(ipSeg)
		tempInt = tempInt << pos
		ipInt = ipInt | tempInt
		pos -= 8
	}
	return ipInt
}

func addrIntToStr(ipInt int) string {
	ipArr := make([]string, 4)
	length := len(ipArr)
	buffer := bytes.NewBufferString("")
	for i := 0; i < length; i++ {
		tempInt := ipInt & 0xFF
		ipArr[length-i-1] = strconv.Itoa(tempInt)
		ipInt = ipInt >> 8
	}
	for i := 0; i < length; i++ {
		buffer.WriteString(ipArr[i])
		if i < length-1 {
			buffer.WriteString(".")
		}
	}
	return buffer.String()
}

func maskCheck(mask string) bool {
	maskInt, err := strconv.Atoi(mask)
	if err != nil {
		return false
	}
	if maskInt > 32 || maskInt < 0 {
		return false
	}
	return true
}

func StringIpToInt(ipstring string) int {
	ipSegs := strings.Split(ipstring, ".")
	var ipInt = 0
	var pos uint = 24
	for _, ipSeg := range ipSegs {
		tempInt, _ := strconv.Atoi(ipSeg)
		tempInt = tempInt << pos
		ipInt = ipInt | tempInt
		pos -= 8
	}
	return ipInt
}
