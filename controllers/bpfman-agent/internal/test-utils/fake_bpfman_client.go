/*
Copyright 2025.

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

package testutils

import (
	"context"
	"fmt"

	gobpfman "github.com/bpfman/bpfman/clients/gobpfman/v1"
	grpc "google.golang.org/grpc"
)

type BpfmanClientFake struct {
	LoadRequests         map[int]*gobpfman.LoadRequest
	UnloadRequests       map[int]*gobpfman.UnloadRequest
	ListRequests         []*gobpfman.ListRequest
	GetRequests          map[int]*gobpfman.GetRequest
	Programs             map[int]*gobpfman.GetResponse
	Links                map[int]bool
	PullBytecodeRequests map[int]*gobpfman.PullBytecodeRequest
}

func NewBpfmanClientFake() *BpfmanClientFake {
	return &BpfmanClientFake{
		LoadRequests:         map[int]*gobpfman.LoadRequest{},
		UnloadRequests:       map[int]*gobpfman.UnloadRequest{},
		ListRequests:         []*gobpfman.ListRequest{},
		GetRequests:          map[int]*gobpfman.GetRequest{},
		Programs:             map[int]*gobpfman.GetResponse{},
		Links:                map[int]bool{},
		PullBytecodeRequests: map[int]*gobpfman.PullBytecodeRequest{},
	}
}

func NewBpfmanClientFakeWithPrograms(programs map[int]*gobpfman.GetResponse) *BpfmanClientFake {
	return &BpfmanClientFake{
		LoadRequests:         map[int]*gobpfman.LoadRequest{},
		UnloadRequests:       map[int]*gobpfman.UnloadRequest{},
		ListRequests:         []*gobpfman.ListRequest{},
		GetRequests:          map[int]*gobpfman.GetRequest{},
		Programs:             programs,
		Links:                map[int]bool{},
		PullBytecodeRequests: map[int]*gobpfman.PullBytecodeRequest{},
	}
}

var currentID = 1000

func (b *BpfmanClientFake) Load(ctx context.Context, in *gobpfman.LoadRequest, opts ...grpc.CallOption) (*gobpfman.LoadResponse, error) {

	loadResponse := &gobpfman.LoadResponse{}
	programs := make([]*gobpfman.LoadResponseInfo, 0)

	for _, prog := range in.Info {
		currentID++
		id := currentID
		progName := prog.Name
		loadResponseInfo := &gobpfman.LoadResponseInfo{
			Info: &gobpfman.ProgramInfo{
				Name: progName,
			},
			KernelInfo: &gobpfman.KernelProgramInfo{
				Id:   uint32(currentID),
				Name: progName,
			},
		}
		programs = append(programs, loadResponseInfo)

		b.Programs[id] = loadRequestToGetResult(loadResponseInfo)
	}
	loadResponse.Programs = programs

	return loadResponse, nil
}

func (b *BpfmanClientFake) Unload(ctx context.Context, in *gobpfman.UnloadRequest, opts ...grpc.CallOption) (*gobpfman.UnloadResponse, error) {
	b.UnloadRequests[int(in.Id)] = in
	delete(b.Programs, int(in.Id))

	return &gobpfman.UnloadResponse{}, nil
}

func loadRequestToGetResult(info *gobpfman.LoadResponseInfo) *gobpfman.GetResponse {
	return &gobpfman.GetResponse{
		Info:       info.Info,
		KernelInfo: info.KernelInfo,
	}
}

func (b *BpfmanClientFake) Get(ctx context.Context, in *gobpfman.GetRequest, opts ...grpc.CallOption) (*gobpfman.GetResponse, error) {
	if b.Programs[int(in.Id)] != nil {
		return &gobpfman.GetResponse{
			Info:       b.Programs[int(in.Id)].Info,
			KernelInfo: b.Programs[int(in.Id)].KernelInfo,
		}, nil
	} else {
		return nil, fmt.Errorf("requested program does not exist")
	}
}

func (b *BpfmanClientFake) PullBytecode(ctx context.Context, in *gobpfman.PullBytecodeRequest, opts ...grpc.CallOption) (*gobpfman.PullBytecodeResponse, error) {
	return &gobpfman.PullBytecodeResponse{}, nil
}

func (b *BpfmanClientFake) List(ctx context.Context, in *gobpfman.ListRequest, opts ...grpc.CallOption) (*gobpfman.ListResponse, error) {
	return &gobpfman.ListResponse{}, nil
}

var currentLinkID = 1000

func (b *BpfmanClientFake) Attach(ctx context.Context, in *gobpfman.AttachRequest, opts ...grpc.CallOption) (*gobpfman.AttachResponse, error) {
	currentLinkID++
	b.Links[currentLinkID] = true
	b.Programs[int(in.Id)].Info.Links = append(b.Programs[int(in.Id)].Info.Links, uint32(currentLinkID))
	return &gobpfman.AttachResponse{
		LinkId: uint32(currentLinkID),
	}, nil
}

func (b *BpfmanClientFake) Detach(ctx context.Context, in *gobpfman.DetachRequest, opts ...grpc.CallOption) (*gobpfman.DetachResponse, error) {
	delete(b.Links, int(in.LinkId))
	return &gobpfman.DetachResponse{}, nil
}
