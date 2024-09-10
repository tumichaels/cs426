# Short Answers

1. Unbuffered channels can contain only 1 message at a time and block on send / receive. Buffered channels can contain up to n messages and only block when sending to a full buffer / reading from empty buffer.

2. *Unbuffered* channels are the default in go

3. The code deadlocks because the goroutine blocks on send.

4. <-chan, chan<- T, and chan T are read-only, write-only, and read-write channels respectively

5. Reading from a closed channel fails silently. However, reading from a nil channel causes deadlock.

6. The for loop terminates only if the channel is closed.

7. You can determine if a `context.Context` is done or cancelled by calling the `Done()` function and checking if the resulting channel is open.

8. The code only prints "all done!". This is because goroutines are non-preemptive. The main function does not relinquish control unless it sleeps or performs I/O. Therefore, the main function terminates before spawned goroutines have a chance to run, thus cancelling them.


9. We can use a `sync.WaitGroup` to wait for all the goroutines to finish

10. Semaphores notify all the workers waiting on them, while mutexes do not.

11. The code prints: 
```
[]
0
true

0
<nil>
{}
```

12. The `struct{}` type is an empty type (it requires no memory). We would want to use `chan struct{}` when we want to use a channel as a signal.

13. In future, preemptive versions of Go, the fix might cause deadlock, as the 3 goroutines would finish before wait is called.