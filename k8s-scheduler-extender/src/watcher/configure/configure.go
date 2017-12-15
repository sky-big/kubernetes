package configure

import (
	"flag"
	"github.com/go-yaml/yaml"
	"github.com/golang/glog"
	"io/ioutil"
)

type Configure struct {
	DiskCollectorInspectPath   string `yaml:"disk-collector-inspect-path"`
	DiskCollectorCheckInterval int    `yaml:"disk-collecotr-check-interval"`
	KubeConfig                 string `yaml:"kube-config"`
}

var Conf *Configure

func LoadConfig(filename string) (*Configure, error) {
	conf := new(Configure)

	b, err := ioutil.ReadFile(filename)
	if err != nil {
		return nil, err
	}
	err = yaml.Unmarshal(b, &conf)
	if err != nil {
		return nil, err
	}

	return conf, nil
}

func InitConfig() {
	configFile := flag.String("config", "/storage-watcher/conf/watcher.conf", "kubernetes watcher configure file path")
	flag.Parse()

	var err error
	Conf, err = LoadConfig(*configFile)
	if err != nil {
		glog.Fatalf("Fail to log configure file - %s", err.Error())
	}
}
