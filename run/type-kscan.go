package run

import (
	"encoding/json"
	"fmt"
	"github.com/lcvvvv/gonmap"
	"github.com/lcvvvv/gonmap/lib/httpfinger"
	"github.com/lcvvvv/gonmap/lib/urlparse"
	"kscan/app"
	"kscan/core/cdn"
	"kscan/core/hydra"
	"kscan/core/slog"
	"kscan/lib/IP"
	"kscan/lib/color"
	"kscan/lib/misc"
	"kscan/lib/pool"
	"kscan/lib/queue"
	"kscan/lib/smap"
	"os"
	"path"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type kscan struct {
	target *queue.Queue
	result *queue.Queue
	config app.Config
	pool   struct {
		host struct {
			icmp *pool.Pool
			tcp  *pool.Pool
			Out  chan interface{}
		}
		port struct {
			tcp *pool.Pool
			Out chan interface{}
		}
		tcpBanner struct {
			tcp *pool.Pool
			Out chan interface{}
		}
		appBanner *pool.Pool
	}
	watchDog struct {
		output  chan interface{}
		hydra   chan interface{}
		wg      *sync.WaitGroup
		trigger bool
	}
	hydra struct {
		pool  *pool.Pool
		queue *queue.Queue
		done  bool
	}

	portScanMap *smap.SMap
}

func New(config app.Config) *kscan {
	k := &kscan{
		target: queue.New(),
		config: config,
		result: queue.New(),
	}

	hostThreads := len(k.config.HostTarget)
	hostThreads = hostThreads/5 + 1
	if hostThreads > 400 {
		hostThreads = 400
	}

	k.pool.appBanner = pool.NewPool(config.Threads)

	k.pool.tcpBanner.tcp = pool.NewPool(config.Threads)
	k.pool.tcpBanner.Out = make(chan interface{})

	k.pool.port.tcp = pool.NewPool(config.Threads * 4)
	k.pool.port.Out = make(chan interface{})

	k.pool.host.icmp = pool.NewPool(hostThreads)
	k.pool.host.tcp = pool.NewPool(hostThreads)
	k.pool.host.Out = make(chan interface{})

	k.pool.appBanner.Interval = time.Microsecond * 500

	k.pool.tcpBanner.tcp.Interval = time.Microsecond * 500
	k.pool.port.tcp.Interval = time.Microsecond * 500

	k.watchDog.hydra = make(chan interface{})
	k.watchDog.output = make(chan interface{})
	k.watchDog.wg = &sync.WaitGroup{}
	k.watchDog.trigger = false

	k.hydra.pool = pool.NewPool(10)
	k.hydra.queue = queue.New()
	k.hydra.done = false

	k.portScanMap = smap.New()
	return k
}

func (k *kscan) HostDiscovery(hostArr []string, open bool) {
	k.pool.host.icmp.Function = func(i interface{}) interface{} {
		ip := i.(string)
		host := NewHost(ip, len(k.config.Port))
		//?????????????????????????????????????????????IP??????
		if open == true {
			return host.Up()
		}
		//?????????????????????????????????IP???????????????????????????
		if gonmap.HostDiscoveryForIcmp(ip) == true {
			slog.Println(slog.DEBUG, host.addr, " is alive")
			return host.Up()
		}
		//ICMP???????????????????????????????????????TCP???????????????
		k.pool.host.tcp.In <- ip
		return host.Down()
	}
	k.pool.host.tcp.Function = func(i interface{}) interface{} {
		ip := i.(string)
		host := NewHost(ip, len(k.config.Port))
		//?????????????????????????????????IP???????????????????????????
		if gonmap.HostDiscoveryForTcp(ip) == true {
			slog.Println(slog.DEBUG, host.addr, " is alive")
			return host.Up()
		}
		return host.Down()
	}
	//??????????????????????????????????????????
	go func() {
		//ICMP???????????????
		wg := &sync.WaitGroup{}
		wg.Add(2)
		go func() {
			for out := range k.pool.host.icmp.Out {
				host := out.(*Host)
				if host.IsAlive() {
					k.pool.host.Out <- out
				}
			}
			wg.Done()
		}()
		//TCP???????????????
		go func() {
			for out := range k.pool.host.tcp.Out {
				host := out.(*Host)
				if host.IsAlive() {
					k.pool.host.Out <- out
				}
			}
			wg.Done()
		}()
		wg.Wait()
		close(k.pool.host.Out)
	}()

	//??????ICMP????????????????????????????????????
	go func() {
		for _, host := range hostArr {
			var ip = host
			if app.Setting.CloseCDN == false {
				var ok bool
				var result string
				var err error
				var isIP = IP.IsIP(host)
				if isIP {
					ok, result, err = cdn.FindWithIP(host)
				} else {
					ok, result, err = cdn.FindWithDomain(host)
				}
				if ok == true {
					url := fmt.Sprintf("cdn://%s", host)
					output := fmt.Sprintf("%-30v %-26v %s", url, "IsCDN", color.RandomImportant(result))
					k.watchDog.output <- output
					continue
				}
				if err != nil {
					slog.Println(slog.DEBUG, err)
				}
				if isIP == false {
					r, err := cdn.Resolution(host)
					if err != nil {
						slog.Println(slog.DEBUG, err)
						continue
					}
					ip = r
					if misc.IsInStrArr(k.config.HostTarget, ip) == true {
						continue
					}
				}
			}
			k.pool.host.icmp.In <- ip
		}
		//???????????????????????????????????????
		if k.config.ClosePing == false {
			slog.Println(slog.INFO, "???????????????????????????????????????")
		}
		k.pool.host.icmp.InDone()
	}()

	//???????????????????????????????????????
	k.pool.host.tcp.RunBack()
	k.pool.host.icmp.Run()
	k.pool.host.tcp.InDone()
	k.pool.host.tcp.Wait()
	if k.config.ClosePing == false {
		slog.Println(slog.WARN, "?????????????????????????????????")
	}
}

func (k *kscan) PortDiscovery() {
	//??????????????????????????????????????????
	go func() {
		var wg int32 = 0
		var threads = k.config.Threads

		for out := range k.pool.host.Out {
			host := out.(*Host)
			k.portScanMap.Set(host.addr, host)
			atomic.AddInt32(&wg, 1)
			go func() {
				defer func() { atomic.AddInt32(&wg, -1) }()
				for _, port := range k.config.Port {
					netloc := NewPort(host.addr, port)
					k.pool.port.tcp.In <- netloc
				}
			}()
			for int(wg) >= threads {
				time.Sleep(1 * time.Second)
			}
		}
		for wg > 0 {
			time.Sleep(1 * time.Second)
		}
		slog.Println(slog.INFO, "???????????????????????????????????????")
		k.pool.port.tcp.InDone()
	}()
	//??????????????????????????????????????????
	go func() {
		for out := range k.pool.port.tcp.Out {
			port := out.(*Port)
			value, _ := k.portScanMap.Get(port.addr)
			host := value.(*Host)
			host.SetAlivePort(port.port, port.status)

			if port.status == Open {
				k.pool.port.Out <- port
				host.Up()
			}
			if port.status == Unknown {
				k.pool.port.Out <- port
			}
			if host.Map.Port.Length() == host.Length.Port {
				//?????????????????????????????????????????????????????????????????????
				host.FinishPortScan()
				//???????????????????????????????????????
				if host.IsOpenPort() == false && k.config.ClosePing == false {
					url := fmt.Sprintf("icmp://%s", host.addr)
					description := color.Red(color.Overturn("Not Open Any Port"))
					output := fmt.Sprintf("%-30v %-26v %s", url, "Up", description)
					k.watchDog.output <- output
				}
			}
			k.portScanMap.Set(port.addr, host)

		}
		close(k.pool.port.Out)
	}()
	//?????????????????????????????????
	k.pool.port.tcp.Function = func(i interface{}) interface{} {
		netloc := i.(*Port)
		if netloc.port == 161 || netloc.port == 137 {
			return netloc.Unknown()
		}
		if gonmap.PortScan("tcp", netloc.addr, netloc.port, 1*time.Second) {
			slog.Println(slog.DEBUG, netloc.UnParse(), " is open")
			return netloc.Open()
		}
		return netloc.Close()
	}
	//???????????????????????????????????????
	k.pool.port.tcp.Run()
	slog.Println(slog.WARN, "?????????????????????????????????")
}

func (k *kscan) GetTcpBanner() {
	k.pool.tcpBanner.tcp.Function = func(i interface{}) interface{} {
		port := i.(*Port)
		var r = gonmap.NewTcpBanner(port.addr, port.port)
		//slog.Println(slog.DEBUG, port.UnParse(),port.Status())
		if port.status == Close {
			return r.CLOSED()
		}
		return gonmap.GetTcpBanner(port.addr, port.port, gonmap.New(), k.config.Timeout*20)
	}

	//??????TCP?????????????????????????????????
	go func() {
		for out := range k.pool.port.Out {
			k.pool.tcpBanner.tcp.In <- out
		}
		slog.Println(slog.INFO, "TCP?????????????????????????????????")
		k.pool.tcpBanner.tcp.InDone()
	}()

	//??????TCP??????????????????????????????
	go func() {
		for out := range k.pool.tcpBanner.tcp.Out {
			//???????????????????????????
			if out == nil {
				continue
			}
			tcpBanner := out.(*gonmap.TcpBanner)
			//???????????????????????????
			if tcpBanner == nil {
				continue
			}

			//??????????????????????????????????????????????????????

			port := tcpBanner.Target.Port()
			addr := tcpBanner.Target.Addr()

			value, _ := k.portScanMap.Get(addr)
			host := value.(*Host)

			if tcpBanner.Status() == gonmap.Matched {
				//slog.Printf(slog.DEBUG, "%s:%d %s %s", addr, port, status, service)
				k.pool.tcpBanner.Out <- tcpBanner
			} else {
				if (tcpBanner.Target.Port() == 161 || tcpBanner.Target.Port() == 137) && tcpBanner.Response.Length() == 0 {
					tcpBanner.CLOSED()
				}
				//slog.Println(slog.WARN, tcpBanner.Target.URI(), tcpBanner.StatusDisplay())
			}
			host.Map.Tcp.Set(port, tcpBanner)

			k.portScanMap.Set(addr, host)
			if host.PortScanIsFinish() == false {
				continue
			}
			if host.Map.Tcp.Length() == host.Length.Tcp && host.CountUnknownPorts() > 0 {
				host.status.tcpScan = true
				k.watchDog.output <- host.DisplayUnknownPorts()
			}
		}
		k.portScanMap.Range(
			func(key, value interface{}) bool {
				host := value.(*Host)
				if host.CountUnknownPorts() == 0 {
					return true
				}
				if host.status.tcpScan == false {
					k.watchDog.output <- host.DisplayUnknownPorts()
				}
				return true
			},
		)
		close(k.pool.tcpBanner.Out)
	}()

	//????????????TCP????????????????????????
	k.pool.tcpBanner.tcp.Run()
	slog.Println(slog.WARN, "TCP???????????????????????????")

}

func (k *kscan) GetAppBanner() {
	k.pool.appBanner.Function = func(i interface{}) interface{} {
		var r *gonmap.AppBanner
		switch i.(type) {
		case string:
			url, _ := urlparse.Load(i.(string))
			r = gonmap.GetAppBannerFromUrl(url)
		case *gonmap.TcpBanner:
			tcpBanner := i.(*gonmap.TcpBanner)
			if tcpBanner == nil {
				return nil
			}
			r = gonmap.GetAppBannerFromTcpBanner(tcpBanner)
		}
		return r
	}

	//appBanner??????????????????????????????????????????
	isDone := make(chan bool)
	go func() {
		i := 0
		for range isDone {
			i++
			if i == 2 {
				break
			}
		}
		k.pool.appBanner.InDone()
		slog.Println(slog.INFO, "???????????????????????????????????????")
	}()

	//??????Url???????????????
	go func() {
		for _, url := range k.config.UrlTarget {
			k.pool.appBanner.In <- url
		}
		isDone <- true
	}()

	//??????App?????????????????????????????????
	go func() {
		for out := range k.pool.tcpBanner.Out {
			tcpBanner := out.(*gonmap.TcpBanner)
			if tcpBanner.Status() != gonmap.Matched {
				continue
			}
			k.pool.appBanner.In <- out
		}
		isDone <- true
	}()

	//????????????App????????????????????????
	k.pool.appBanner.Run()
	slog.Println(slog.WARN, "?????????????????????????????????")
}

func (k *kscan) GetAppBannerFromCheck() {
	k.pool.appBanner.Function = func(i interface{}) interface{} {
		var r *gonmap.AppBanner
		switch i.(type) {
		case string:
			url, _ := urlparse.Load(i.(string))
			r = gonmap.GetAppBannerFromUrl(url)
		case *gonmap.TcpBanner:
			tcpBanner := i.(*gonmap.TcpBanner)
			r = gonmap.GetAppBannerFromTcpBanner(tcpBanner)
		}
		return r
	}

	//??????Url???????????????
	go func() {
		for _, url := range k.config.UrlTarget {
			k.pool.appBanner.In <- url
		}
		k.pool.appBanner.InDone()
		slog.Println(slog.INFO, "???????????????????????????????????????")
	}()

	//????????????App????????????????????????
	k.pool.appBanner.Run()
	slog.Println(slog.WARN, "?????????????????????????????????")
}

func (k *kscan) Output() {
	//????????????????????????
	var bannerMapArr []map[string]string
	for out := range k.watchDog.output {
		if out == nil {
			continue
		}
		var disp string
		var write string
		//???????????????,????????????????????????????????????????????????
		k.watchDog.trigger = true
		//????????????
		switch out.(type) {
		case *gonmap.AppBanner:
			banner := out.(*gonmap.AppBanner)
			if banner == nil {
				continue
			}
			if app.Setting.Match != "" && strings.Contains(banner.Response, app.Setting.Match) == false {
				continue
			}
			bannerMapArr = append(bannerMapArr, banner.Map())
			if app.Setting.CloseCDN == false {
				result, _ := cdn.Find(banner.IPAddr)
				if result != "" {
					banner.AddFingerPrint("Attribution", result)

				}
			}
			write = outputTcpBanner(banner, app.Setting.CloseColor)
			disp = displayTcpBanner(banner, app.Setting.CloseColor)
		case hydra.AuthInfo:
			info := out.(hydra.AuthInfo)
			if info.Status == false {
				continue
			}
			write = info.Output()
			disp = info.Display()
		case string:
			outString := out.(string)
			write = outString
			disp = outString
		}
		slog.Println(slog.DATA, disp)
		if k.config.Output != nil {
			k.config.WriteLine(write)
		}
	}
	//??????json
	if app.Setting.OutputJson != "" {
		fileName := app.Setting.OutputJson
		if _, err := os.Stat(fileName); os.IsNotExist(err) {
			_ = os.MkdirAll(path.Dir(fileName), os.ModePerm)
		}
		bytes, _ := json.Marshal(bannerMapArr)
		err := misc.WriteLine(fileName, bytes)
		if err == nil {
			slog.Printf(slog.INFO, "???????????????Json?????????????????????", fileName)
		} else {
			slog.Println(slog.WARN, "??????Json????????????????????????", err.Error())
		}
	}

	if len(httpfinger.NewKeywords) > 0 {
		newKeywords := misc.RemoveDuplicateElement(httpfinger.NewKeywords)
		slog.Println(slog.WARN, "?????????kscan?????????????????????finger.txt???????????????????????????Github")
		dir, _ := os.Getwd()
		slog.Printf(slog.WARN, "????????????http??????[%d]???:%s/%s", len(newKeywords), dir, "finger.txt")
		data := strings.Join(newKeywords, "\r\n")
		_ = misc.WriteLine("finger.txt", []byte(data))
	}

}

func (k *kscan) WatchDog() {
	k.watchDog.wg.Add(1)
	//?????????????????????
	waitTime := 30 * time.Second
	//??????????????????????????????????????????????????????????????????
	go func() {
		for true {
			time.Sleep(waitTime)
			if k.watchDog.trigger == false {
				slog.Printf(slog.WARN,
					"?????????????????????:??????????????????????????????%d??????,??????????????????????????????%d??????,TCP??????????????????%d??????,APP??????????????????%d??????",
					k.pool.host.icmp.JobsList.Length()+k.pool.host.tcp.JobsList.Length(),
					k.pool.port.tcp.JobsList.Length(),
					k.pool.tcpBanner.tcp.JobsList.Length(),
					k.pool.appBanner.JobsList.Length(),
				)
			}
		}
	}()
	time.Sleep(time.Millisecond * 500)
	//??????????????????????????????????????????????????????
	go func() {
		for true {
			time.Sleep(waitTime)
			k.watchDog.trigger = false
		}
	}()
	//Hydra??????
	if app.Setting.Hydra {
		k.watchDog.wg.Add(1)
	}

	for out := range k.pool.appBanner.Out {
		k.watchDog.output <- out
		if app.Setting.Hydra {
			k.watchDog.hydra <- out
		}
	}

	k.watchDog.wg.Done()
	close(k.watchDog.hydra)

	k.watchDog.wg.Wait()
	close(k.watchDog.output)
}

func (k *kscan) Hydra() {
	slog.Println(slog.INFO, "hydra????????????????????????????????????????????????")
	slog.Println(slog.WARN, "??????????????????hydra????????????", misc.Intersection(hydra.ProtocolList, app.Setting.HydraMod))
	//???????????????????????????
	hydra.InitDefaultAuthMap()
	//?????????????????????
	hydra.InitCustomAuthMap(app.Setting.HydraUser, app.Setting.HydraPass)
	//???????????????
	k.hydra.pool.Function = func(i interface{}) interface{} {
		if i == nil {
			return nil
		}
		banner := i.(*gonmap.AppBanner)
		//??????????????????
		authInfo := hydra.NewAuthInfo(banner.IPAddr, banner.Port, banner.Protocol)
		crack := hydra.NewCracker(authInfo, app.Setting.HydraUpdate, 10)
		slog.Printf(slog.INFO, "[hydra]->?????????%v:%v[%v]???????????????????????????????????????%d", banner.IPAddr, banner.Port, banner.Protocol, crack.Length())
		go crack.Run()
		//??????????????????
		var out hydra.AuthInfo
		for info := range crack.Out {
			out = info
		}
		return out
	}
	//???????????????????????????
	go func() {
		for out := range k.watchDog.hydra {
			if out == nil {
				continue
			}
			banner := out.(*gonmap.AppBanner)
			if banner == nil {
				continue
			}
			if misc.IsInStrArr(app.Setting.HydraMod, banner.Protocol) == false {
				continue
			}
			if hydra.Ok(banner.Protocol) == false {
				continue
			}
			k.hydra.queue.Push(banner)
		}
		k.hydra.done = true
	}()
	//???????????????????????????
	go func() {
		var TargetMap = make(map[string][]string)
		for true {
			if k.hydra.queue.Len() == 0 && k.hydra.done == true {
				break
			}
			pop := k.hydra.queue.Pop()
			if pop == nil {
				continue
			}
			banner := pop.(*gonmap.AppBanner)
			//???????????????????????????????????????????????????
			if _, ok := TargetMap[banner.Netloc()]; ok == false {
				TargetMap[banner.Netloc()] = []string{banner.Protocol}
				k.hydra.pool.In <- banner
				continue
			}
			protocolArr := TargetMap[banner.Netloc()]
			if misc.IsInStrArr(protocolArr, banner.Protocol) == false {
				if arr := []string{"rdp", "smb"}; misc.IsInStrArr(arr, banner.Protocol) {
					TargetMap[banner.Netloc()] = append(protocolArr, arr...)
					k.hydra.pool.In <- banner
					continue
				}
				if arr := []string{"pop3", "smtp", "imap"}; misc.IsInStrArr(arr, banner.Protocol) {
					TargetMap[banner.Netloc()] = append(protocolArr, arr...)
					k.hydra.pool.In <- banner
					continue
				}
				TargetMap[banner.Netloc()] = append(protocolArr, banner.Protocol)
				k.hydra.pool.In <- banner
				continue
			}
		}
		//??????????????????
		k.hydra.pool.InDone()
	}()
	//???????????????????????????
	go func() {
		for out := range k.hydra.pool.Out {
			k.watchDog.output <- out
		}
		k.watchDog.wg.Done()
	}()

	k.hydra.pool.Run()
}

func displayTcpBanner(appBanner *gonmap.AppBanner, keyPrint bool) string {
	m := misc.FixMap(appBanner.FingerPrint())
	fingerPrint := color.StrMapRandomColor(m, keyPrint, []string{"ProductName", "Hostname", "DeviceType"}, []string{"ApplicationComponent"})
	fingerPrint = misc.FixLine(fingerPrint)
	format := "%-30v %-" + strconv.Itoa(misc.AutoWidth(appBanner.AppDigest, 26)) + "v %s"
	s := fmt.Sprintf(format, appBanner.URL(), appBanner.AppDigest, fingerPrint)
	return s
}

func outputTcpBanner(appBanner *gonmap.AppBanner, keyPrint bool) string {
	fingerPrint := misc.StrMap2Str(appBanner.FingerPrint(), keyPrint)
	fingerPrint = misc.FixLine(fingerPrint)
	s := fmt.Sprintf("%s\t%d\t%s\t%s", appBanner.URL(), appBanner.StatusCode, appBanner.AppDigest, fingerPrint)
	return s
}
