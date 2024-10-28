package raft

//
// this is an outline of the API that raft must expose to
// the service (or tester). see comments below for
// each of these functions for more details.
//
// rf = Make(...)
//   create a new Raft server.
// rf.Start(command interface{}) (index, term, isleader)
//   start agreement on a new log entry
// rf.GetState() (term, isLeader)
//   ask a Raft for its current term, and whether it thinks it is leader
// ApplyMsg
//   each time a new entry is committed to the log, each Raft peer
//   should send an ApplyMsg to the service (or tester)
//   in the same server.
//

import (
	//	"bytes"

	"bytes"
	"fmt"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"

	//	"6.824/labgob"
	"6.824/labgob"
	"6.824/labrpc"
)

type ApplyMsg struct {
	CommandValid bool
	Command      interface{}
	CommandIndex int
}

type Raft struct {
	mu        sync.Mutex          // Lock to protect shared access to this peer's state
	peers     []*labrpc.ClientEnd // RPC end points of all peers
	persister *Persister          // Object to hold this peer's persisted state
	me        int                 // this peer's index into peers[]
	dead      int32               // set by Kill()

	CurrentTerm int
	VotedFor    int // null value is -1
	Log         Log

	lastApplied int
	commitIndex int

	nextIndex  []int
	matchIndex []int

	role             RaftRole
	electionDeadline time.Time
	leaderID         int

	applyCh chan<- ApplyMsg
}

////////////////////////////////////////////////////////////////////////////////
//																			  //
//							PERSISTENCE FUNCTIONS						      //
//																			  //
////////////////////////////////////////////////////////////////////////////////

func (rf *Raft) persist() {
	w := new(bytes.Buffer)
	e := labgob.NewEncoder(w)
	e.Encode(rf.CurrentTerm)
	e.Encode(rf.VotedFor)
	e.Encode(rf.Log)
	data := w.Bytes()
	rf.persister.SaveRaftState(data)
}

func (rf *Raft) readPersist(data []byte) {
	if data == nil || len(data) < 1 { // bootstrap without any state?
		return
	}
	r := bytes.NewBuffer(data)
	d := labgob.NewDecoder(r)
	var currentTerm int
	var votedFor int
	var log *InMemoryLog
	if d.Decode(&currentTerm) != nil ||
		d.Decode(&votedFor) != nil ||
		d.Decode(&log) != nil {
		DPrintfFromNode(rf.me, "Error reading from persistent state")
	} else {
		fmt.Printf("server %d reading from persisten state", rf.me)
		DPrintfFromNode(rf.me, "persistent state success entry")
		rf.CurrentTerm = currentTerm
		rf.VotedFor = votedFor
		rf.Log = log
		if rf.Log.GetLastLogIndex() > 0 {
			DPrintfFromNode(rf.me, "\tfirst entry: %v", rf.Log.GetEntry(1))
		}
	}
}

////////////////////////////////////////////////////////////////////////////////
//																			  //
//							   ELECTION FUNCTIONS						      //
//																			  //
////////////////////////////////////////////////////////////////////////////////

type RequestVoteArgs struct {
	Term         int
	CandidateID  int
	LastLogIndex int
	LastLogTerm  int
}

type RequestVoteReply struct {
	Term        int
	VoteGranted bool
}

func (rf *Raft) RequestVote(args *RequestVoteArgs, reply *RequestVoteReply) {
	rf.mu.Lock()
	defer rf.mu.Unlock()

	// my term is out of date (if leader or candidate, revert to follower)
	if args.Term > rf.CurrentTerm {
		rf.revertToFollower(args.Term)
	}

	lastLogIndex := rf.Log.GetLastLogIndex()
	lastLogTerm := rf.Log.GetEntry(lastLogIndex).Term
	logIsOk := (args.LastLogTerm > lastLogTerm) || ((args.LastLogTerm == lastLogTerm) && args.LastLogIndex >= lastLogIndex)

	if rf.CurrentTerm == args.Term { // is the vote from this term (not earlier)
		if rf.VotedFor == -1 && logIsOk { // can i vote for this guy?
			DPrintfFromNode(rf.me, "\tvoted for server %d", args.CandidateID)
			rf.VotedFor = args.CandidateID
			rf.persist()
			rf.resetElectionDeadline()
		}
	}

	reply.Term = rf.CurrentTerm
	reply.VoteGranted = (rf.VotedFor == args.CandidateID)
}

func (rf *Raft) startNewElection() (votesNeeded int, voteCounter chan *RequestVoteReply) {
	rf.CurrentTerm++
	rf.VotedFor = rf.me
	rf.persist()
	rf.resetElectionDeadline()
	DPrintfFromNode(rf.me, "started election in term %d", rf.CurrentTerm)

	voteCounter = make(chan *RequestVoteReply, len(rf.peers))

	for server := range len(rf.peers) {
		if server == rf.me {
			continue
		}
		lastLogIndex := rf.Log.GetLastLogIndex()
		lastLogTerm := rf.Log.GetEntry(lastLogIndex).Term
		args := RequestVoteArgs{rf.CurrentTerm, rf.me, lastLogIndex, lastLogTerm}
		reply := RequestVoteReply{}

		go func(rf *Raft, server int, voteCounter chan<- *RequestVoteReply, args *RequestVoteArgs, reply *RequestVoteReply) {
			if rf.peers[server].Call("Raft.RequestVote", args, reply) {
				voteCounter <- reply
			}
		}(rf, server, voteCounter, &args, &reply)
	}

	return (len(rf.peers) + 1) / 2, voteCounter
}

func (rf *Raft) countVotes(votesNeeded int, voteCounter <-chan *RequestVoteReply) {
	votesReceived := 1
	for range len(rf.peers) - 1 { // -1 because you personally don't vote
		replyPtr := <-voteCounter // each vote counter corresponds to one term of rpc CALLS (not responses)
		reply := *replyPtr

		// lock to access state variables
		rf.mu.Lock()

		if reply.Term > rf.CurrentTerm {
			rf.revertToFollower(reply.Term)
			rf.mu.Unlock()
			return
		}
		if reply.VoteGranted {
			votesReceived++
		}
		if votesReceived >= votesNeeded {
			rf.becomeLeader()
			rf.mu.Unlock()
			return
		}

		rf.mu.Unlock()
	}
}

func (rf *Raft) becomeLeader() {
	DPrintf("server %d became leader in term %d", rf.me, rf.CurrentTerm)
	rf.role = Leader

	for server := range len(rf.peers) {
		if server == rf.me {
			continue
		}
		rf.nextIndex[server] = rf.Log.GetLastLogIndex() + 1
		rf.matchIndex[server] = 0
	}

	go rf.sendHeartBeats()
	go rf.startAgreement()
	go rf.leaderCommitThread()
}

func (rf *Raft) sendHeartBeats() {
	for !rf.killed() {
		rf.mu.Lock()
		if rf.role != Leader { // Check rf.role with the lock
			rf.mu.Unlock()
			return
		}
		for i, server := range rf.peers {
			if i == rf.me {
				continue
			}
			prevLogIndex := rf.Log.GetLastLogIndex()
			prevLogTerm := rf.Log.GetEntry(prevLogIndex).Term
			args := AppendEntriesArgs{rf.CurrentTerm, rf.me, prevLogIndex, prevLogTerm, []Entry{}, rf.commitIndex}
			reply := AppendEntriesReply{}

			go func(rf *Raft, server *labrpc.ClientEnd, args *AppendEntriesArgs, reply *AppendEntriesReply) {
				server.Call("Raft.AppendEntries", args, reply)
			}(rf, server, &args, &reply)
		}
		rf.mu.Unlock()
		time.Sleep(time.Duration(200) * time.Millisecond)
	}
	DPrintf("server %d heartbeat stopped", rf.me)
}

func (rf *Raft) leaderCommitThread() {
	for rf.killed() == false {
		rf.mu.Lock()
		if rf.role != Leader {
			rf.mu.Unlock()
			return
		}

		lastIndex := rf.Log.GetLastLogIndex()
		for N := lastIndex; N > rf.commitIndex; N-- {
			count := 1 // Start with this server's own log
			for i := range rf.peers {
				if i != rf.me && rf.matchIndex[i] >= N {
					count++
				}
			}

			if count > len(rf.peers)/2 && rf.Log.GetEntry(N).Term == rf.CurrentTerm {
				rf.commitIndex = N
				// DPrintfFromNode(rf.me, "committed index %d", N)
				break
			}
		}
		rf.mu.Unlock()
		time.Sleep(time.Duration(80+rand.Intn(41)) * time.Millisecond)
	}
}

func (rf *Raft) ticker() {
	for !rf.killed() {
		rf.mu.Lock()
		beforeElectionStart := time.Now().Before(rf.electionDeadline)

		if beforeElectionStart {
			rf.mu.Unlock()
			time.Sleep(time.Duration(90+rand.Intn(21)) * time.Millisecond)
		} else if rf.role != Leader {
			votesNeeded, voteCounter := rf.startNewElection()
			rf.mu.Unlock()
			go rf.countVotes(votesNeeded, voteCounter)
		} else {
			rf.mu.Unlock()
		}
	}
}

////////////////////////////////////////////////////////////////////////////////
//																			  //
//							   AGREEMENT FUNCTIONS						      //
//																			  //
////////////////////////////////////////////////////////////////////////////////

type AppendEntriesArgs struct {
	Term         int
	LeaderID     int
	PrevLogIndex int
	PrevLogTerm  int
	Entries      []Entry
	LeaderCommit int
}

type AppendEntriesReply struct {
	Term                      int
	Success                   bool
	ConflictingEntryTerm      int
	ConflictingTermFirstIndex int
}

func (rf *Raft) AppendEntries(args *AppendEntriesArgs, reply *AppendEntriesReply) {
	rf.mu.Lock()
	defer rf.mu.Unlock()

	// msg out of date
	if args.Term < rf.CurrentTerm {
		reply.Term = rf.CurrentTerm
		reply.Success = false
		return
	}

	// become follower?
	if args.Term >= rf.CurrentTerm {
		rf.revertToFollower(args.Term)
		rf.leaderID = args.LeaderID
		if len(args.Entries) == 0 && rf.leaderID == -1 {
			DPrintfFromNode(rf.me, "\trecognized server %d as leader", args.LeaderID)
		}
	}

	// leader term is up to date
	reply.Term = rf.CurrentTerm
	rf.resetElectionDeadline()

	// prevlogindex must be in my log (no gaps)
	if rf.Log.GetLastLogIndex() < args.PrevLogIndex {
		reply.Success = false
		return
	}

	// prevlogindex item term must match!
	myPrevLogTerm := rf.Log.GetEntry(args.PrevLogIndex).Term
	if myPrevLogTerm != args.PrevLogTerm {
		reply.Success = false
		reply.ConflictingEntryTerm = myPrevLogTerm
		reply.ConflictingTermFirstIndex = rf.Log.GetFirstIndex(myPrevLogTerm, args.PrevLogIndex)
		return
	}

	reply.Success = true

	// iterate and fill...
	for i, entry := range args.Entries {
		idx := args.PrevLogIndex + 1 + i
		if rf.Log.GetLastLogIndex() >= idx && rf.Log.GetEntry(idx).Term == entry.Term {
			continue

		} else {
			rf.Log.DeleteEntriesFrom(idx)
			rf.Log.AppendEntries(args.Entries[i:])
			rf.persist()
			break
		}
	}

	// commit?
	if args.LeaderCommit > rf.commitIndex {
		lastNewIndex := rf.Log.GetLastLogIndex()
		if lastNewIndex < args.LeaderCommit {
			rf.commitIndex = lastNewIndex
		} else {
			rf.commitIndex = args.LeaderCommit
		}
	}
}

func (rf *Raft) startAgreement() {
	rf.mu.Lock()
	defer rf.mu.Unlock()

	for server := range rf.peers {
		if server == rf.me {
			continue
		}

		go rf.agreementThread(server)
	}
}

func (rf *Raft) agreementThread(server int) {
	for rf.killed() == false {
		rf.mu.Lock()
		if rf.role != Leader {
			rf.mu.Unlock()
			return
		}

		lastLogIndex := rf.Log.GetLastLogIndex()

		if lastLogIndex < rf.nextIndex[server] {
			rf.mu.Unlock()
			time.Sleep(time.Duration(80+rand.Intn(41)) * time.Millisecond) // high contention maybe
			continue

		} else {
			args := AppendEntriesArgs{
				Term:         rf.CurrentTerm,
				LeaderID:     rf.me,
				PrevLogIndex: rf.nextIndex[server] - 1,
				PrevLogTerm:  rf.Log.GetEntry(rf.nextIndex[server] - 1).Term,
				Entries:      rf.Log.GetEntriesStartingFrom(rf.nextIndex[server]),
				LeaderCommit: rf.commitIndex,
			}
			reply := AppendEntriesReply{}
			rf.mu.Unlock()

			if rf.peers[server].Call("Raft.AppendEntries", &args, &reply) == true {
				rf.mu.Lock()
				if reply.Success {
					DPrintfFromNode(rf.me, "\t\tsuccess server %d added %d entries after %d in term %d", server, len(args.Entries), args.PrevLogIndex, rf.CurrentTerm)
					rf.nextIndex[server] = args.PrevLogIndex + len(args.Entries) + 1
					rf.matchIndex[server] = args.PrevLogIndex + len(args.Entries)

				} else if reply.Term > rf.CurrentTerm {
					rf.revertToFollower(reply.Term)
				} else {
					DPrintfFromNode(rf.me, "\t\tfailure server %d didn't add %d entries after %d in term %d", server, len(args.Entries), args.PrevLogIndex, rf.CurrentTerm)
					rf.nextIndex[server]--
					// if my term is higher -->
					// 		if my index for that term is in that range, send that
					//      else first
					// if my term is lower --> jump to before first index for term
				}
				rf.mu.Unlock()
			}
		}
	}
}

func (rf *Raft) checkAgreement(logsNeeded int, logCounter <-chan struct{}) {

}

// On conversion to candidate, start election:
//   - Increment currentTerm
//   - Vote for self
//   - Reset election timer
//   - Send RequestVote RPCs to all other servers
//
// If votes received from majority of servers: become leader
// If AppendEntries RPC received from new leader: convert to follower
// If election timeout elapses: start new election
func (rf *Raft) GetState() (int, bool) {
	rf.mu.Lock()
	defer rf.mu.Unlock()
	return rf.CurrentTerm, rf.role == Leader
}

func (rf *Raft) Start(command interface{}) (index int, term int, isLeader bool) {
	rf.mu.Lock()
	defer rf.mu.Unlock()

	index = rf.Log.GetLastLogIndex() + 1
	term = rf.CurrentTerm
	isLeader = false

	if rf.role == Leader {
		isLeader = true
		rf.Log.AppendEntry(Entry{command, term})
		rf.persist()
	}

	return index, term, isLeader
}

func (rf *Raft) Kill() {
	atomic.StoreInt32(&rf.dead, 1)
}

func (rf *Raft) killed() bool {
	z := atomic.LoadInt32(&rf.dead)
	return z == 1
}

func Make(peers []*labrpc.ClientEnd, me int,
	persister *Persister, applyCh chan ApplyMsg) *Raft {
	rf := &Raft{}
	rf.peers = peers
	rf.persister = persister
	rf.me = me

	rf.CurrentTerm = 0
	rf.VotedFor = -1
	rf.Log = MakeInMemoryLog()

	rf.nextIndex = make([]int, len(peers))
	rf.matchIndex = make([]int, len(peers))

	rf.role = Follower
	rf.resetElectionDeadline()
	rf.leaderID = -1

	rf.applyCh = applyCh

	rf.readPersist(persister.ReadRaftState())

	go rf.ticker()
	go rf.applyThread()
	return rf
}

////////////////////////////////////////////////////////////////////////////////
//																			  //
//							   APPLYING FUNCTIONS						      //
//																			  //
////////////////////////////////////////////////////////////////////////////////

func (rf *Raft) applyThread() {
	for rf.killed() == false {
		rf.mu.Lock()

		for idx := rf.lastApplied + 1; idx <= rf.commitIndex; idx++ {
			rf.applyCh <- ApplyMsg{true, rf.Log.GetEntry(idx).Data, idx}
			rf.lastApplied = idx
			// DPrintfFromNode(rf.me, "applied idx %d %v", idx, rf.Log.GetEntry(idx))
			fmt.Printf("server %d applied idx %d %v\n", rf.me, idx, rf.Log.GetEntry(idx))
		}

		rf.mu.Unlock()
		time.Sleep(time.Duration(80+rand.Intn(41)) * time.Millisecond)
	}
}

// -------------------- general utility functions --------------------

func (rf *Raft) resetElectionDeadline() {
	rf.electionDeadline = time.Now().Add(time.Duration(490+rand.Intn(21)) * time.Millisecond)
}

func (rf *Raft) revertToFollower(newTerm int) {
	if newTerm > rf.CurrentTerm {
		DPrintfFromNode(rf.me, "reverted to follower in term %d", rf.CurrentTerm)
		rf.CurrentTerm = newTerm
		rf.persist()
		rf.VotedFor = -1
		rf.persist()
		rf.role = Follower
	} else {
		rf.role = Follower
	}
}
