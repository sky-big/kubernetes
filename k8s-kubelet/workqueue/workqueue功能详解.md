# workqueue功能详解

## 场景分析

* workqueue主要是为了可以并发处理里面需要处理的对象,workqueue里面的元素可以并行加入到队列里面
* workqueue 提供的一个保障就是，如果是同一个object，比如同一个 container，被多次加到 workqueue 里，在 dequeue 时，它只会出现一次。防止会有同一个 object 被多个 worker 同时处理
* 在提供上面这些基础的服务之后在workqueue的基础queue上面继续封装了rate limited queue,该功能是避免一个对象处理失败之后立刻放入到队列里面,这样会出现hot loop,而是会根据limiter返回的等待时间间隔将object加入到队列中

* 如果没有使用workqueue会出现的问题:

	(1).启动的多个groutine可能出现并发的处理同一个对象,比如replicaset controller,当一个groutine计算出自己需要增加的容器个数,然后另外一个groutine开始更新操作,这个groutine仍然计算出需要增加那么多的容器个数,然后两个groutine启动了比预想更多的容器,然后多个容器增加事件过来之后又会触发多个groutine去更新,多个groutine发现多了容器,然后每个groutine又减少同样多的容器,导致容器个数少于预期,于是又会触发增加操作,这样很容器导致死循环

	(2).如果是每个组件用到的时候去给每个启动的groutine里面添加对应资源的channel,通过这样
	的方式来避免多个groutine处理同一个object,这样每个groutine里面跟资源以及channel紧密相联系,而workqueue提供的这种中间件的方式解除来这种耦合性

* 结论:是需要一个公共的组件实现上面所提出来的功能。

## 功能点概括

* 代码存在于k8s.io/client-go/util/workqueue目录;
* workqueue提供的功能点一:如果是同一个object,被多次加入到workqueue里,在dequeue时,它只会出现一次,防止会有同一个object被多个worker同时处理;
* workqueue提供的功能点二:rate limited,如果你从workqueue里面拿出一个object,处理时发生了错误,重新放回到workqueue,这时workqueue保证这个
  object不会立刻重新处理,防止hot loop(即入队列会有速率限制)
* workqueue提供的功能点三:workqueue提供prometheus监控,你可以实时监控queue的长度,延迟等性能指标,你可以监控queue的处理速度是否能跟的上

## 功能点一的实现(k8s.io/client-go/util/workqueue/queue.go):

* queue的数据结构详解:

```
// workqueue队列数据结构
type Type struct {
	// queue defines the order in which we will work on items. Every
	// element of queue should be in the dirty set and not in the
	// processing set.
	// 存储队列里面的数据集合
	queue []t

	// dirty defines all of the items that need to be processed.
	// 等待处理的队列数据集合,该集合主要是对应processing集合中的队列数据
	// 如果队列object入队列时processing队列中有该object,则将该object不放入queue队列而是放入到dirty集合
	// 等processing队列里面的object回调done表示object处理完成后再将元素放入到queue里面
	dirty set

	// Things that are currently being processed are in the processing set.
	// These things may be simultaneously in the dirty set. When we finish
	// processing something and remove it from this set, we'll check if
	// it's in the dirty set, and if so, add it to the queue.
	// 正在进行处理中的object集合
	processing set

	// 条件等待锁
	cond *sync.Cond

	// 标识队列是否关闭
	shuttingDown bool

	// 统计监控队列
	metrics queueMetrics
}
```

* workqueue添加元素操作:

```
// 队列中添加元素
func (q *Type) Add(item interface{}) {
	q.cond.L.Lock()
	defer q.cond.L.Unlock()
	// 如果队列处于终止状态则直接返回
	if q.shuttingDown {
		return
	}
	// 如果dirty集合中存在元素则直接返回,workqueue队列不同于普通的queue,workqueue里面的元素是表示
	// 该元素需要进行更新处理,相同的元素则是可以直接进行覆盖
	if q.dirty.has(item) {
		return
	}

	// 将元素添加到监控统计中
	q.metrics.add(item)

	// 执行到此处表明dirty中没有该元素则将元素添加到dirty集合中
	q.dirty.insert(item)
	// 如果该元素正在进行处理中,则直接返回
	if q.processing.has(item) {
		return
	}

	// 如果该元素没有进行处理中,则将元素添加到queue中
	q.queue = append(q.queue, item)
	// 发送信号让等待获取元素的groutine可以进行工作了
	q.cond.Signal()
}
```

* workqueue获取元素操作:

```
func (q *Type) Get() (item interface{}, shutdown bool) {
	q.cond.L.Lock()
	defer q.cond.L.Unlock()
	// 如果队列元素为0同时队列没有关闭则阻塞等待
	for len(q.queue) == 0 && !q.shuttingDown {
		q.cond.Wait()
	}
	if len(q.queue) == 0 {
		// We must be shutting down.
		return nil, true
	}

	// 从queue中获取元素
	item, q.queue = q.queue[0], q.queue[1:]

	// 通知监控有元素被获取
	q.metrics.get(item)

	// 将元素添加到processing集合中
	q.processing.insert(item)
	// 将元素从dirty集合中删除
	q.dirty.delete(item)

	return item, false
}
```

* workqueue回调表明object操作完毕的操作:

```
// 回调通知元素被处理完毕
func (q *Type) Done(item interface{}) {
	q.cond.L.Lock()
	defer q.cond.L.Unlock()

	// 通知监控元素处理完毕
	q.metrics.done(item)

	// 从processing集合中将元素删除
	q.processing.delete(item)
	// 如果dirty集合中有该元素表明在元素的处理过程中有新的更新过来,则将该元素加入到queue队列
	// 然后发出队列有元素的信号
	if q.dirty.has(item) {
		q.queue = append(q.queue, item)
		q.cond.Signal()
	}
}
```

## 功能点二的实现(k8s.io/client-go/util/workqueue/rate_limiting_queue.go):

* 实现原理是速率控制器返回元素延迟进入队列的时间,然后将这个时间交给延迟队列延迟入队列
* 实现延迟队列以及速率限制的接口如下:

```
// 队列速率限制器接口
type RateLimitingInterface interface {
	// 延迟队列接口
	DelayingInterface

	// AddRateLimited adds an item to the workqueue after the rate limiter says its ok
	// 将元素延迟加入到队列中
	AddRateLimited(item interface{})

	// Forget indicates that an item is finished being retried.  Doesn't matter whether its for perm failing
	// or for success, we'll stop the rate limiter from tracking it.  This only clears the `rateLimiter`, you
	// still have to call `Done` on the queue.
	// 擦除元素的延迟记录
	Forget(item interface{})

	// NumRequeues returns back how many times the item was requeued
	// 获取某个元素最大的失败次数
	NumRequeues(item interface{}) int
}
```

* 实现上面接口对象:

```
// 实现速率限制队列接口的对象
type rateLimitingType struct {
	DelayingInterface

	rateLimiter RateLimiter
}

// AddRateLimited AddAfter's the item based on the time when the rate limiter says its ok
func (q *rateLimitingType) AddRateLimited(item interface{}) {
	q.DelayingInterface.AddAfter(item, q.rateLimiter.When(item))
}

func (q *rateLimitingType) NumRequeues(item interface{}) int {
	return q.rateLimiter.NumRequeues(item)
}

func (q *rateLimitingType) Forget(item interface{}) {
	q.rateLimiter.Forget(item)
}
```

* 速率限制器接口定义(k8s.io/client-go/util/workqueue/default_rate_limiters.go)

```
type RateLimiter interface {
	// When gets an item and gets to decide how long that item should wait
	// 获得元素需要等待多长时间加入到queue队列中
	// 每次调用该接口的时候表明该元素失败一次,然后将失败次数累加一次
	When(item interface{}) time.Duration
	// Forget indicates that an item is finished being retried.  Doesn't matter whether its for perm failing
	// or for success, we'll stop tracking it
	// 通知ratelimiter擦出某个元素的记录
	Forget(item interface{})
	// NumRequeues returns back how many failures the item has had
	// 获取元素当前失败处理的次数
	NumRequeues(item interface{}) int
}
```

* 在k8s.io/client-go/workqueue/default_rate_limiters.go中默认提供了几种限速器,同时提供了一个速率限制器集合对象,该集合器集合了
  多个速率控制器
* 该速率控制器集合同时实现了速率控制器接口
  
```
// 速率控制器集合
type MaxOfRateLimiter struct {
	limiters []RateLimiter
}

// 获得所有速率控制器中返回的延迟入队列的时间最大值,同时每个速率控制器会对该元素的失败次数累加一次
func (r *MaxOfRateLimiter) When(item interface{}) time.Duration {
	ret := time.Duration(0)
	for _, limiter := range r.limiters {
		curr := limiter.When(item)
		if curr > ret {
			ret = curr
		}
	}

	return ret
}

func NewMaxOfRateLimiter(limiters ...RateLimiter) RateLimiter {
	return &MaxOfRateLimiter{limiters: limiters}
}

// 返回一个元素在所有速率控制器中失败的最大次数
func (r *MaxOfRateLimiter) NumRequeues(item interface{}) int {
	ret := 0
	for _, limiter := range r.limiters {
		curr := limiter.NumRequeues(item)
		if curr > ret {
			ret = curr
		}
	}

	return ret
}

// 从所有的速率控制器中擦除元素
func (r *MaxOfRateLimiter) Forget(item interface{}) {
	for _, limiter := range r.limiters {
		limiter.Forget(item)
	}
}
```
