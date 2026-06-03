package render

import (
	"image"
	"runtime"
	"sync"
	"zen-board/internal/model"
)

type FrameJob struct {
	Index  int
	Events []model.FrameEvent
	Cam    CameraState
	Style  string
}

type RenderResult struct {
	Index int
	Frame *image.RGBA
}

type RenderPool struct {
	Workers    int
	Jobs       chan FrameJob
	Results    chan RenderResult
	BufferPool *sync.Pool
}

func NewRenderPool(width, height int) *RenderPool {
	workers := runtime.NumCPU()
	return &RenderPool{
		Workers: workers,
		Jobs:    make(chan FrameJob, workers*2),
		Results: make(chan RenderResult, workers*2),
		BufferPool: &sync.Pool{
			New: func() interface{} {
				return image.NewRGBA(image.Rect(0, 0, width, height))
			},
		},
	}
}

// In engine.go we will launch the workers and feed jobs.
