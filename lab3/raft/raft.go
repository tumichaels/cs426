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

	"math/rand"
	"sync"
	"sync/atomic"
	"time"

	//	"6.824/labgob"
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

	role             RaftRole
	electionDeadline time.Time

	leaderID int
}

func (rf *Raft) persist() {
}

func (rf *Raft) readPersist(data []byte) {
	if data == nil || len(data) < 1 { // bootstrap without any state?
		return
	}
}

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
		rf.stepDown(args.Term)
	}

	lastLogIndex := rf.Log.GetLastLogIndex()
	lastLogTerm := rf.Log.GetEntry(lastLogIndex).Term
	logIsOk := (args.Term > lastLogTerm) || ((args.Term == lastLogTerm) && args.LastLogIndex >= lastLogIndex)

	if rf.CurrentTerm == args.Term { // is the vote from this term (not earlier)
		if rf.VotedFor == -1 && logIsOk { // can i vote for this guy?
			DPrintf("server %d voted for server %d", rf.me, args.CandidateID)
			rf.VotedFor = args.CandidateID
			rf.resetElectionDeadline()
		}
	}

	reply.Term = rf.CurrentTerm
	reply.VoteGranted = (rf.VotedFor == args.CandidateID)
}

type AppendEntriesArgs struct {
	Term         int
	LeaderID     int
	PrevLogIndex int
	PrevLogTerm  int
	Entries      []Entry
	LeaderCommit int
}

type AppendEntriesReply struct {
	Term    int
	Success bool
}

func (rf *Raft) AppendEntries(args *AppendEntriesArgs, reply *AppendEntriesReply) {
	rf.mu.Lock()
	defer rf.mu.Unlock()

	// stale request
	if args.Term < rf.CurrentTerm {
		reply.Term = rf.CurrentTerm
		reply.Success = false
		return
	}

	// become follower?
	if args.Term >= rf.CurrentTerm {
		rf.stepDown(args.Term)
		rf.leaderID = args.LeaderID // this might be incorrect?
		DPrintf("server %d recognized server %d as leader in term %d", rf.me, rf.leaderID, rf.CurrentTerm)
	}

	// update for this term
	reply.Term = rf.CurrentTerm
	rf.resetElectionDeadline()

	lastLogIndex := rf.Log.GetLastLogIndex()
	// no gap in log
	if args.PrevLogIndex > lastLogIndex {
		reply.Success = false
		return
	}

	// term of prev log must match
	if rf.Log.GetEntry(lastLogIndex).Term != args.PrevLogTerm {
		reply.Success = false
		return
	}

	// we succeded here
	reply.Success = true

	for i, e := range args.Entries {
		index := args.PrevLogIndex + 1 + i
		if rf.Log.GetEntry(index).Term == e.Term {
			continue
		}

		// fill in!
		rf.Log.DeleteEntriesFrom(index)
		for _, entry := range args.Entries[i:] {
			rf.Log.AppendEntry(entry)
		}
		break
	}

	// min of leaderCommit and last index
	if args.LeaderCommit > rf.commitIndex {
		rf.commitIndex = rf.Log.GetLastLogIndex()
		if args.LeaderCommit < rf.commitIndex {
			rf.commitIndex = args.LeaderCommit
		}
	}
}

func (rf *Raft) startNewElection() (voteCounter chan *RequestVoteReply) {
	rf.CurrentTerm++
	rf.VotedFor = rf.me
	rf.resetElectionDeadline()
	DPrintf("server %d started election in term %d", rf.me, rf.CurrentTerm)

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

	return voteCounter
}

func (rf *Raft) countVotes(votesNeeded int, voteCounter <-chan *RequestVoteReply) {
	votesReceived := 1
	for range len(rf.peers) {
		replyPtr := <-voteCounter
		reply := *replyPtr
		rf.mu.Lock() // just lock outside?
		if reply.Term > rf.CurrentTerm {
			rf.stepDown(reply.Term)
			rf.mu.Unlock()
			return
		} else if reply.VoteGranted {
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

	go rf.sendHeartBeats()
}

func (rf *Raft) sendHeartBeats() {
	for !rf.killed() {
		rf.mu.Lock()
		if rf.role != Leader { // Check rf.role with the lock
			rf.mu.Unlock()
			return
		}
		for server := range rf.peers {
			if server == rf.me {
				continue
			}
			prevLogIndex := rf.Log.GetLastLogIndex()
			prevLogTerm := rf.Log.GetEntry(prevLogIndex).Term
			args := AppendEntriesArgs{rf.CurrentTerm, rf.me, prevLogIndex, prevLogTerm, []Entry{}, rf.commitIndex}
			reply := AppendEntriesReply{}

			go func(rf *Raft, server int, args *AppendEntriesArgs, reply *AppendEntriesReply) {
				rf.peers[server].Call("Raft.AppendEntries", args, reply)
			}(rf, server, &args, &reply)
		}
		rf.mu.Unlock()
		time.Sleep(time.Duration(200) * time.Millisecond)
	}
	DPrintf("server %d heartbeat stopped", rf.me)
}

func (rf *Raft) killed() bool {
	z := atomic.LoadInt32(&rf.dead)
	return z == 1
}

func (rf *Raft) ticker() {
	for rf.killed() == false {
		rf.mu.Lock()
		if time.Now().Before(rf.electionDeadline) {
			rf.mu.Unlock()
			time.Sleep(time.Duration(90+rand.Intn(21)) * time.Millisecond)
		} else if rf.role != Leader {
			voteCounter := rf.startNewElection()
			rf.mu.Unlock()
			go rf.countVotes((len(rf.peers)+1)/2, voteCounter)
		} else {
			rf.mu.Unlock()
		}
	}
}

func (rf *Raft) GetState() (int, bool) {
	rf.mu.Lock()
	defer rf.mu.Unlock()
	return rf.CurrentTerm, rf.role == Leader
}

func (rf *Raft) Start(command interface{}) (int, int, bool) {
	return -1, -1, false
}

func (rf *Raft) Kill() {
	atomic.StoreInt32(&rf.dead, 1)
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

	rf.role = Follower
	rf.resetElectionDeadline()
	rf.leaderID = -1

	rf.readPersist(persister.ReadRaftState())
	go rf.ticker()
	return rf
}

func (rf *Raft) resetElectionDeadline() {
	rf.electionDeadline = time.Now().Add(time.Duration(490+rand.Intn(21)) * time.Millisecond)
}

func (rf *Raft) stepDown(newTerm int) {
	// transition from candidate / leader to follower
	if rf.CurrentTerm < newTerm {
		rf.CurrentTerm = newTerm
		rf.VotedFor = -1
		rf.role = Follower
	} else {
		rf.role = Follower
	}
}
