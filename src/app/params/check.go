package params

import (
	"lib/misc"
	"lib/slog"
	"os"
	"regexp"
)

var OutPutFile *os.File

func initOutPutFile(path string) *os.File {
	return misc.SafeOpen(path)
}

func checkParams() {
	//判断冲突参数
	if Params.port != "" && Params.top != 0 {
		slog.Error("PORT、TOP只允许同时出现一个")
	}
	//判断内容
	if Params.target != "" {
		if Params.port != "" {
			if !checkIntsParam(Params.port) {
				slog.Error("PORT参数输入错误,其格式应为80，8080，8081-8090")
			}
		}
		if Params.top != 0 {
			if Params.top > 1000 || Params.top < 1 {
				slog.Error("TOP参数输入错误,TOP参数应为1-1000之间的整数。")
			}
		}
		if Params.output != "" {
			//验证output参数
			f := initOutPutFile(Params.output)
			OutPutFile = f
		}
		if Params.proxy != "" {
			if !checkProxyParam(Params.proxy) {
				slog.Error("PROXY参数输入错误，其格式应为：http://IP:PORT，支持socks5/4")
			}
		}
		if Params.path != "" {
			if !checkStringsParam(Params.path) {
				slog.Error("PATH参数输入错误，其格式应为：/asdfasdf，可使用逗号输入多个路径")
			}
		}
		if Params.host != "" {
			//验证host参数
		}
		if Params.threads != 0 {
			//验证threads参数
		}
		if Params.timeout != 3 {
			//验证timeout参数
		}
		if Params.httpCode != "" {
			if !checkIntsParam(Params.httpCode) {
				slog.Error("HTTPCODE参数输入错误，其格式应为200可用逗号输入多个状态码")
			}
		}
	} else {
		slog.Error("必须输入TARGET参数")
	}
}

func checkIntsParam(v string) bool {
	matched, err := regexp.MatchString("^((?:[0-9])+(?:-[0-9]+)?)(?:,(?:[0-9])+-(?:[0-9])+)*$", v)
	if err != nil {
		return false
	}
	if matched {
		return true
	} else {
		return false
	}
}

func checkStringsParam(v string) bool {
	matched, err := regexp.MatchString("^([A-Za-z0-9/]+)(,[A-Za-z0-9/])*$", v)
	if err != nil {
		return false
	}
	if matched {
		return true
	} else {
		return false
	}
}

func checkProxyParam(v string) bool {
	matched, err := regexp.MatchString("^(http|https|socks5|socks4)://[0-9.]+:[0-9]+$", v)
	if err != nil {
		return false
	}
	if matched {
		return true
	} else {
		return false
	}
}
