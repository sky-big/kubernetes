/*
Copyright 2014 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package factory

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/golang/glog"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/kubernetes/plugin/pkg/scheduler/algorithm"
	"k8s.io/kubernetes/plugin/pkg/scheduler/algorithm/predicates"
	"k8s.io/kubernetes/plugin/pkg/scheduler/algorithm/priorities"
	schedulerapi "k8s.io/kubernetes/plugin/pkg/scheduler/api"
)

// PluginFactoryArgs are passed to all plugin factory functions.
// 所有选择策略需要的工厂参数结构
type PluginFactoryArgs struct {
	PodLister                      algorithm.PodLister
	ServiceLister                  algorithm.ServiceLister
	ControllerLister               algorithm.ControllerLister
	ReplicaSetLister               algorithm.ReplicaSetLister
	StatefulSetLister              algorithm.StatefulSetLister
	NodeLister                     algorithm.NodeLister
	NodeInfo                       predicates.NodeInfo
	PVInfo                         predicates.PersistentVolumeInfo
	PVCInfo                        predicates.PersistentVolumeClaimInfo
	HardPodAffinitySymmetricWeight int
}

// MetadataProducerFactory produces MetadataProducer from the given args.
// 元数据工厂方法
type MetadataProducerFactory func(PluginFactoryArgs) algorithm.MetadataProducer

// A FitPredicateFactory produces a FitPredicate from the given args.
// 预选策略封装的工厂方法
type FitPredicateFactory func(PluginFactoryArgs) algorithm.FitPredicate

// DEPRECATED
// Use Map-Reduce pattern for priority functions.
// A PriorityFunctionFactory produces a PriorityConfig from the given args.
// 优选策略封装的工厂方法
type PriorityFunctionFactory func(PluginFactoryArgs) algorithm.PriorityFunction

// A PriorityFunctionFactory produces map & reduce priority functions
// from a given args.
// FIXME: Rename to PriorityFunctionFactory.
type PriorityFunctionFactory2 func(PluginFactoryArgs) (algorithm.PriorityMapFunction, algorithm.PriorityReduceFunction)

// A PriorityConfigFactory produces a PriorityConfig from the given function and weight
type PriorityConfigFactory struct {
	Function          PriorityFunctionFactory
	MapReduceFunction PriorityFunctionFactory2
	Weight            int
}

var (
	// 预选,优选策略,调度器map的锁
	schedulerFactoryMutex sync.Mutex

	// maps that hold registered algorithm types
	// 所有的预选策略对应的方法
	fitPredicateMap      = make(map[string]FitPredicateFactory)
	// 所有的优选策略对应的方法
	priorityFunctionMap  = make(map[string]PriorityConfigFactory)
	// 所有的调度器对应的map,每个调度器对应一堆预选和优选策略,对应的这些策略都是在上面的预选和优选集合中的子集
	algorithmProviderMap = make(map[string]AlgorithmProviderConfig)

	// Registered metadata producers
	// 优选策略元数据生产者
	priorityMetadataProducer  MetadataProducerFactory
	// 预选策略元数据生产者
	predicateMetadataProducer MetadataProducerFactory

	// get equivalence pod function
	getEquivalencePodFunc algorithm.GetEquivalencePodFunc
)

const (
	// 默认的调度器名字
	DefaultProvider = "DefaultProvider"
)

type AlgorithmProviderConfig struct {
	FitPredicateKeys     sets.String
	PriorityFunctionKeys sets.String
}

// RegisterFitPredicate registers a fit predicate with the algorithm
// registry. Returns the name with which the predicate was registered.
// 不需要工厂参数的预选调度策略注册接口,需要在此处进行统一封装一层,然后进行统一的注册
func RegisterFitPredicate(name string, predicate algorithm.FitPredicate) string {
	return RegisterFitPredicateFactory(name, func(PluginFactoryArgs) algorithm.FitPredicate { return predicate })
}

// RegisterFitPredicateFactory registers a fit predicate factory with the
// algorithm registry. Returns the name with which the predicate was registered.
// 所有的预选策略方法都需要在此处进行注册
func RegisterFitPredicateFactory(name string, predicateFactory FitPredicateFactory) string {
	schedulerFactoryMutex.Lock()
	defer schedulerFactoryMutex.Unlock()
	// 验证预选策略名字的正确性
	validateAlgorithmNameOrDie(name)
	// 将预选策略添加到所有预选策略的map中
	fitPredicateMap[name] = predicateFactory
	return name
}

// RegisterCustomFitPredicate registers a custom fit predicate with the algorithm registry.
// Returns the name, with which the predicate was registered.
// 根据配置文件中的预选策略进行注册
func RegisterCustomFitPredicate(policy schedulerapi.PredicatePolicy) string {
	var predicateFactory FitPredicateFactory
	var ok bool

	// 验证单个策略的配置是否正确
	validatePredicateOrDie(policy)

	// generate the predicate function, if a custom type is requested
	if policy.Argument != nil {
		if policy.Argument.ServiceAffinity != nil {
			predicateFactory = func(args PluginFactoryArgs) algorithm.FitPredicate {
				predicate, precomputationFunction := predicates.NewServiceAffinityPredicate(
					args.PodLister,
					args.ServiceLister,
					args.NodeInfo,
					policy.Argument.ServiceAffinity.Labels,
				)

				// Once we generate the predicate we should also Register the Precomputation
				predicates.RegisterPredicatePrecomputation(policy.Name, precomputationFunction)
				return predicate
			}
			// 配置的预选策略根据节点标签来选择是否当前节点是否符合条件
		} else if policy.Argument.LabelsPresence != nil {
			predicateFactory = func(args PluginFactoryArgs) algorithm.FitPredicate {
				return predicates.NewNodeLabelPredicate(
					policy.Argument.LabelsPresence.Labels,
					policy.Argument.LabelsPresence.Presence,
				)
			}
		}
		// 配置的预选策略是否scheduler中已经提供的
	} else if predicateFactory, ok = fitPredicateMap[policy.Name]; ok {
		// checking to see if a pre-defined predicate is requested
		glog.V(2).Infof("Predicate type %s already registered, reusing.", policy.Name)
		// 配置的预选策略是已经存在的则不需要进行注册,立刻返回
		return policy.Name
	}

	if predicateFactory == nil {
		glog.Fatalf("Invalid configuration: Predicate type not found for %s", policy.Name)
	}

	// 如果配置的预选策略是新生成的,则需要注册到scheduler系统中
	return RegisterFitPredicateFactory(policy.Name, predicateFactory)
}

// IsFitPredicateRegistered is useful for testing providers.
// 判断预选策略是否已经注册过
func IsFitPredicateRegistered(name string) bool {
	schedulerFactoryMutex.Lock()
	defer schedulerFactoryMutex.Unlock()
	_, ok := fitPredicateMap[name]
	return ok
}

// 注册优选策略元数据生产者工厂
func RegisterPriorityMetadataProducerFactory(factory MetadataProducerFactory) {
	schedulerFactoryMutex.Lock()
	defer schedulerFactoryMutex.Unlock()
	priorityMetadataProducer = factory
}

// 注册预选策略元数据生产者工厂
func RegisterPredicateMetadataProducerFactory(factory MetadataProducerFactory) {
	schedulerFactoryMutex.Lock()
	defer schedulerFactoryMutex.Unlock()
	predicateMetadataProducer = factory
}

// DEPRECATED
// Use Map-Reduce pattern for priority functions.
// Registers a priority function with the algorithm registry. Returns the name,
// with which the function was registered.
// scheduler组件优选策略的注册,不过注册的方法没有进行工厂参数的封装,因此需要对注册的方法用工厂参数进行封装一层
func RegisterPriorityFunction(name string, function algorithm.PriorityFunction, weight int) string {
	return RegisterPriorityConfigFactory(name, PriorityConfigFactory{
		Function: func(PluginFactoryArgs) algorithm.PriorityFunction {
			return function
		},
		Weight: weight,
	})
}

// RegisterPriorityFunction2 registers a priority function with the algorithm registry. Returns the name,
// with which the function was registered.
// FIXME: Rename to PriorityFunctionFactory.
// 优选策略注册mapFunction
func RegisterPriorityFunction2(
	name string,
	mapFunction algorithm.PriorityMapFunction,
	reduceFunction algorithm.PriorityReduceFunction,
	weight int) string {
	return RegisterPriorityConfigFactory(name, PriorityConfigFactory{
		MapReduceFunction: func(PluginFactoryArgs) (algorithm.PriorityMapFunction, algorithm.PriorityReduceFunction) {
			return mapFunction, reduceFunction
		},
		Weight: weight,
	})
}

// 向优选策略集合进行注册优选策略
func RegisterPriorityConfigFactory(name string, pcf PriorityConfigFactory) string {
	schedulerFactoryMutex.Lock()
	defer schedulerFactoryMutex.Unlock()
	// 验证优选策略名字的正确性
	validateAlgorithmNameOrDie(name)
	// 将优选策略添加到scheduler组件的优选策略集合中
	priorityFunctionMap[name] = pcf
	return name
}

// RegisterCustomPriorityFunction registers a custom priority function with the algorithm registry.
// Returns the name, with which the priority function was registered.
// 根据配置文件中配置的优选策略进行注册到scheduler策略系统中
func RegisterCustomPriorityFunction(policy schedulerapi.PriorityPolicy) string {
	var pcf *PriorityConfigFactory

	// 判断配置的优选策略是否正确
	validatePriorityOrDie(policy)

	// generate the priority function, if a custom priority is requested
	if policy.Argument != nil {
		// 如果节点service服务下的pod有对应的标签则权重加重的优选策略
		if policy.Argument.ServiceAntiAffinity != nil {
			pcf = &PriorityConfigFactory{
				Function: func(args PluginFactoryArgs) algorithm.PriorityFunction {
					return priorities.NewServiceAntiAffinityPriority(
						args.PodLister,
						args.ServiceLister,
						policy.Argument.ServiceAntiAffinity.Label,
					)
				},
				Weight: policy.Weight,
			}
			// 根据节点有无对应标签加重或者权重的优选策略
		} else if policy.Argument.LabelPreference != nil {
			pcf = &PriorityConfigFactory{
				MapReduceFunction: func(args PluginFactoryArgs) (algorithm.PriorityMapFunction, algorithm.PriorityReduceFunction) {
					return priorities.NewNodeLabelPriority(
						policy.Argument.LabelPreference.Label,
						policy.Argument.LabelPreference.Presence,
					)
				},
				Weight: policy.Weight,
			}
		}
		// 配置文件中配置的优选策略在优选集合中存在,则直接根据存在的优选策略组装对应的优选策略
		// 此处配置文件中配置的优选策略会覆盖掉以前对应名字的优选策略,所以覆盖之后用的配置文件中配置的权重值
	} else if existingPcf, ok := priorityFunctionMap[policy.Name]; ok {
		glog.V(2).Infof("Priority type %s already registered, reusing.", policy.Name)
		// set/update the weight based on the policy
		pcf = &PriorityConfigFactory{
			Function:          existingPcf.Function,
			MapReduceFunction: existingPcf.MapReduceFunction,
			Weight:            policy.Weight,
		}
	}

	if pcf == nil {
		glog.Fatalf("Invalid configuration: Priority type not found for %s", policy.Name)
	}

	// 对优选策略进行注册
	return RegisterPriorityConfigFactory(policy.Name, *pcf)
}

func RegisterGetEquivalencePodFunction(equivalenceFunc algorithm.GetEquivalencePodFunc) {
	getEquivalencePodFunc = equivalenceFunc
}

// IsPriorityFunctionRegistered is useful for testing providers.
// 根据优选策略名字判断优选策略是否已经注册过
func IsPriorityFunctionRegistered(name string) bool {
	schedulerFactoryMutex.Lock()
	defer schedulerFactoryMutex.Unlock()
	_, ok := priorityFunctionMap[name]
	return ok
}

// RegisterAlgorithmProvider registers a new algorithm provider with the algorithm registry. This should
// be called from the init function in a provider plugin.
// 根据调度器名字和它对应的预选优选策略集合注册新的调度器
func RegisterAlgorithmProvider(name string, predicateKeys, priorityKeys sets.String) string {
	schedulerFactoryMutex.Lock()
	defer schedulerFactoryMutex.Unlock()
	validateAlgorithmNameOrDie(name)
	algorithmProviderMap[name] = AlgorithmProviderConfig{
		FitPredicateKeys:     predicateKeys,
		PriorityFunctionKeys: priorityKeys,
	}
	return name
}

// GetAlgorithmProvider should not be used to modify providers. It is publicly visible for testing.
// 根据调度器名字得到对应的调度器对象,该对象包括预选和优选策略集合
func GetAlgorithmProvider(name string) (*AlgorithmProviderConfig, error) {
	schedulerFactoryMutex.Lock()
	defer schedulerFactoryMutex.Unlock()

	provider, ok := algorithmProviderMap[name]
	if !ok {
		return nil, fmt.Errorf("plugin %q has not been registered", name)
	}

	return &provider, nil
}

// 根据预选策略名字集合和预选策略需要的参数对象得到预选名字对应的预选方法集合
func getFitPredicateFunctions(names sets.String, args PluginFactoryArgs) (map[string]algorithm.FitPredicate, error) {
	schedulerFactoryMutex.Lock()
	defer schedulerFactoryMutex.Unlock()

	predicates := map[string]algorithm.FitPredicate{}
	for _, name := range names.List() {
		// 根据预选策略名字得到对应的预选产品
		factory, ok := fitPredicateMap[name]
		if !ok {
			return nil, fmt.Errorf("Invalid predicate name %q specified - no corresponding function found", name)
		}
		// 对得到的产品使用产品需要的参数得到对应的预选方法
		predicates[name] = factory(args)
	}
	return predicates, nil
}

// 获得优选策略元数据生产者
func getPriorityMetadataProducer(args PluginFactoryArgs) (algorithm.MetadataProducer, error) {
	schedulerFactoryMutex.Lock()
	defer schedulerFactoryMutex.Unlock()

	if priorityMetadataProducer == nil {
		return algorithm.EmptyMetadataProducer, nil
	}
	return priorityMetadataProducer(args), nil
}

// 获取预选策略元数据生产者
func getPredicateMetadataProducer(args PluginFactoryArgs) (algorithm.MetadataProducer, error) {
	schedulerFactoryMutex.Lock()
	defer schedulerFactoryMutex.Unlock()

	if predicateMetadataProducer == nil {
		return algorithm.EmptyMetadataProducer, nil
	}
	return predicateMetadataProducer(args), nil
}

// 根据给定的预选策略名字集合和工厂产品参数对象得到对应名字的预选方法集合
func getPriorityFunctionConfigs(names sets.String, args PluginFactoryArgs) ([]algorithm.PriorityConfig, error) {
	schedulerFactoryMutex.Lock()
	defer schedulerFactoryMutex.Unlock()

	configs := []algorithm.PriorityConfig{}
	for _, name := range names.List() {
		// 根据优选策略名字得到对应优选策略的工厂产品
		factory, ok := priorityFunctionMap[name]
		if !ok {
			return nil, fmt.Errorf("Invalid priority name %s specified - no corresponding function found", name)
		}
		// 如果工厂产品的Function字段存在,则创建对应优选策略方法
		if factory.Function != nil {
			configs = append(configs, algorithm.PriorityConfig{
				Function: factory.Function(args),
				Weight:   factory.Weight,
			})
			// 如果工厂产品的mapFunction字段存在,则创建对应优选策略方法
		} else {
			mapFunction, reduceFunction := factory.MapReduceFunction(args)
			configs = append(configs, algorithm.PriorityConfig{
				Map:    mapFunction,
				Reduce: reduceFunction,
				Weight: factory.Weight,
			})
		}
	}
	// 验证优选策略集合的正确性(主要检查所有优选集合的权重是否超过全局最大的权重值)
	if err := validateSelectedConfigs(configs); err != nil {
		return nil, err
	}
	return configs, nil
}

// validateSelectedConfigs validates the config weights to avoid the overflow.
// 验证优选策略集合的正确性(主要检查所有优选集合的权重是否超过全局最大的权重值)
func validateSelectedConfigs(configs []algorithm.PriorityConfig) error {
	var totalPriority int
	for _, config := range configs {
		// Checks totalPriority against MaxTotalPriority to avoid overflow
		if config.Weight*schedulerapi.MaxPriority > schedulerapi.MaxTotalPriority-totalPriority {
			return fmt.Errorf("Total priority of priority functions has overflown")
		}
		totalPriority += config.Weight * schedulerapi.MaxPriority
	}
	return nil
}

var validName = regexp.MustCompile("^[a-zA-Z0-9]([-a-zA-Z0-9]*[a-zA-Z0-9])$")

func validateAlgorithmNameOrDie(name string) {
	if !validName.MatchString(name) {
		glog.Fatalf("Algorithm name %v does not match the name validation regexp \"%v\".", name, validName)
	}
}

// 判断配置的预选策略是否正确
func validatePredicateOrDie(predicate schedulerapi.PredicatePolicy) {
	if predicate.Argument != nil {
		numArgs := 0
		if predicate.Argument.ServiceAffinity != nil {
			numArgs++
		}
		if predicate.Argument.LabelsPresence != nil {
			numArgs++
		}
		if numArgs != 1 {
			glog.Fatalf("Exactly 1 predicate argument is required, numArgs: %v, Predicate: %s", numArgs, predicate.Name)
		}
	}
}

// 判断配置的优选策略是否正确
func validatePriorityOrDie(priority schedulerapi.PriorityPolicy) {
	if priority.Argument != nil {
		numArgs := 0
		if priority.Argument.ServiceAntiAffinity != nil {
			numArgs++
		}
		if priority.Argument.LabelPreference != nil {
			numArgs++
		}
		if numArgs != 1 {
			glog.Fatalf("Exactly 1 priority argument is required, numArgs: %v, Priority: %s", numArgs, priority.Name)
		}
	}
}

// 列出所有的预选策略
func ListRegisteredFitPredicates() []string {
	schedulerFactoryMutex.Lock()
	defer schedulerFactoryMutex.Unlock()

	names := []string{}
	for name := range fitPredicateMap {
		names = append(names, name)
	}
	return names
}

// 列出所有的优选策略
func ListRegisteredPriorityFunctions() []string {
	schedulerFactoryMutex.Lock()
	defer schedulerFactoryMutex.Unlock()

	names := []string{}
	for name := range priorityFunctionMap {
		names = append(names, name)
	}
	return names
}

// ListAlgorithmProviders is called when listing all available algorithm providers in `kube-scheduler --help`
// 列出所有的调度器
func ListAlgorithmProviders() string {
	var availableAlgorithmProviders []string
	for name := range algorithmProviderMap {
		availableAlgorithmProviders = append(availableAlgorithmProviders, name)
	}
	sort.Strings(availableAlgorithmProviders)
	return strings.Join(availableAlgorithmProviders, " | ")
}
