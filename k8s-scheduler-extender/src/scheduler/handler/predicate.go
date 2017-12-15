package handler

import (
	"net/http"
	"io/ioutil"
	"encoding/json"

	"github.com/julienschmidt/httprouter"
	"github.com/golang/glog"
	"k8s.io/kubernetes/plugin/pkg/scheduler/api/v1"
)

func (h *Handler) StorageSchedulerPredicate(w http.ResponseWriter, req *http.Request, ps httprouter.Params) error {
	body, err := ioutil.ReadAll(req.Body)
    defer req.Body.Close()
    if err != nil {
    	return err
    }

    // parse scheduler extender send extender args
    var extenderArgs v1.ExtenderArgs
    if err := json.Unmarshal(body, &extenderArgs); err != nil {
		return err
	}
	glog.Infof("StorageSchedulerPredicate Receieve Scheduler Extender Args : %v", extenderArgs.NodeNames)

	var result v1.ExtenderFilterResult
	result.Nodes = extenderArgs.Nodes
	result.NodeNames = extenderArgs.NodeNames

	out, err := json.Marshal(result)
	if err != nil {
		return err
	}
	w.Write(out)

	return nil
}