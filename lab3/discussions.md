## 3A-2

1. The key idea is that nodes must be unable to timeout and vote for themselves before they receive a request vote rpc from a separate node. Suppose we have 3 nodes, 1, 2, 3, where node 1 is the leader, and the timeouts of nodes 2 and 3 are t_2 and t_3 respectively and t_2 < t_3. Let d be the time it takes to send a message across a network. Consider the following scenario:
- node 1 (leader) crashes
- node 2 (earlier timeout) times out, it then votes for itself and sends a request vote rpc to nodes 1 and 2
- since the message arrives at node 3 at time t_2 + d > t_2, node 3 does not vote for node 2, leading to a failed election, similarly node 2 does not vote for node 3
- when nodes 2 and 3 continue to pick timeouts in this manner, the election continues to fail

2. This is not a major concern because in raft deployment, timeout times are typically much longer than the time needed to make a RPC call
