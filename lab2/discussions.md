# B1

If you delete a pod, then the kubernetes control plane will create a new pod with the deployment. What happened is the control plane detected that a pod that was expected was down. Since kubernetes is a declarative system, it automatically restarted the pod to make it match the declared state.

# B4

```
Welcome! You have chosen user ID 204292 (Stokes7398/jonathonschmidt@schmitt.io)

Their recommended videos are:
 1. scenic Amaranth Leaves by Gus Stroman
 2. The zealous gerbil's wood by Rosalia Muller
 3. Beardoes: calculate by Garett Trantow
 4. drab on by Odell Boyer
 5. glamorous Soybeans by Esmeralda Stamm
```



# C3

Since there are two `UserService` and two `VideoService`, I would expect that using a 4 connection pool results in lower latency because the services can process 4 requests simultaneously. I would expect the results to be even better as I increased the size of the connection pool, since then we can pipeline the requests better. I also expect that using fewer connections in the pool would lead to higher latency as we receive fewer requests at a time.