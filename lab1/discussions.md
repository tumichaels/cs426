# A: Basic Functionality

## A1

The `grpc.NewClient` API is based on the BSD socket API, which is the standard for TCP/IP programming in UNIX. (In Windows, there is the winsock API, which is very similar to the BSD API, and allows for easy conversion between the two)

## A2

The `NewClient` call might fail because the services we depend on (`UserService` and `VideoService`) cannot accept new connections right now. This would probably correspond to the `GRPC_STATUS_UNAVAILABLE` error code. In this case, since we depend on a different service, I decided to use the same error code as returned from the call to `NewClient`

## A3 

The `GetUser` function probably uses the `recv` and `sendto` syscalls to receive requests/send responses respectively. We might have errors if the connection is disrupted or we don't have enough space to buffer the messages, especially on the receiving end. The most likely case in which `GetUser` returns an error despite network calls succeeding is malformed inputs or requests. In that case, `GetUser` cannot return valid output, since the data it is asking for is fundamentally impossible to return.

## A4

It is impossible to use the same conn for both `VideoService` and `UserService`. The two servers are listening on different ports, and TCP specifies that only one server can listen to one port at a time. 

## A6

```
2024/09/26 02:06:10 This user has name Stroman2309, their email is genevievehackett@mosciski.biz, and their profile URL is https://user-service.localhost/profile/204674
2024/09/26 02:06:10 Recommended videos:
2024/09/26 02:06:10   [0] Video id=1264, title="quaint Carrot", author=Guiseppe Kshlerin, url=https://video-data.localhost/blob/1264
2024/09/26 02:06:10
```

## A8:
Overall, I believe you should send batched requests concurrently because you will increase you throughput by sending twice as much data at once. Since your requests will be placed into the NIC buffer on the receiver anyways, you're ultimately limited by the speed of the receiving NIC, it's buffer size, and the processing power of the server. So long as we can process the requests at the rate at which they arrive at `UserService/VideoService`, we won't have any issue. However, sending multiple requests at once necessarily means that the listening server will need more processing power. Additionally, it might cause large spikes in network traffic, which is less efficient than sending a steady stream of data. Overall, I believe that it would be better to send all the requests concurrently as our data is not order-dependent. However, this assumes that we're mostly limited by the speed of our network, rather than the processor. My machine for example, uses a 4-core intel i5 chip, and is unable to handle a large number of concurrent requests. Because it is bottlenecked by processing ability rather than network traffic, it would be better for me to receive a steady stream of requests.

# B: Stats collection

## B2

```
now_us	total_requests	total_errors	active_requests	user_service_errors	video_service_errors	average_latency_ms	p99_latency_ms	stale_responses
1727317965310681	658	0	2	0	0	203.77	0.00	0
1727317966316288	668	0	2	0	0	203.77	0.00	0
1727317967318663	678	0	3	0	0	203.84	0.00	0
1727317968308017	688	0	2	0	0	205.28	0.00	0
```

# C: Retries and resiliency

## C1

Retrying might be a bad option over transient unavailability caused by the server being unable to handle a large number of concurrent requests. If the server is nearing capacity and errors a request, retrying causes a backlog of requests, which further overburdens the server, leading to more failed requests. The result is a negative feedback loop where the server, which should have been able to fix itself after pausing the retry requests, is now hoplessly overwhelmed.

## C2

My first decision would be that even if VideoService is down and my responses are expired, I should just return the expired videos and note that they are expired. Configuring the server to use fallback suggests that having a response is more important than the response being up to date. I think the main advantage of doing this is that it gives more power to the client. Rather than the server deciding that its out of date responses are unacceptable, the client can choose whether to wait on `VideoRecService / VideoService` if they really need the most up to date recommendations, or simply use the fall back if that is "good enough". One of the major downsides of this approach is that it might mask larger network issues. If `VideoRecService` returns its fallback information, we may be unable to detect that `VideoService` is down, which will cause much more degraded recommendatiosn over time.

## C3

The most straightforward way of improving reliability of `VideoRecService` is to cache responses from `VideoService` and `UserService`. That way if either of the dependent services is down, we can simply used the cached values. Of course, this introduces a whole new layer of complexity which  only slow down our recommendation service as we strive to maintain cache coherency.

## C4

Connection establishment is fundamentally limited by the amount of time it requires to complete the TCP handshake. Therefore, creating a new connection for each request adds a significant constant overhead. As mentioned earlier, one way to address this is to maintain pools of connections to each services, and distribute requests between them. One downside of doing so, however, is that either the `VideoRecService` needs some load-balancing capability in order to avoid overusing one connection, or we will need an intermmediate load balancer to evenly distribute our requests between our receiving services.

---
## Extra Credit 1:

These types of errors (successful network, errored `GetUser`) will have to be detected by the server which is processing `GetUser` requests.

## Extra Credit 2:

We could potentially reduce the total number of requests to `UserService` and `VideoService` by batching requests to those services from multiple calls to `VideoRecService`. I would do this by:

1. Maintain 2 pools of connections (probably separate goroutines), 1 for service I need
2. Then when a new `GetTopVideo` request comes in, I send it to the connection pool using a channel
    a. while the request is being made, I listen to a response channel

This way, we can batch multiple `UserService` and `VideoService` requests. I'm not entirely sure how to open new channels between the two connection pools and the `GetTopVideo` goroutines, but I think that information (maybe a channel pool) would just have to be stored within the `server` struct.

