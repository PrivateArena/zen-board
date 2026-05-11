package render

import (
	"image"
	"runtime"
	"sync"
)

type FrameJob struct {
	Index int
	// Additional data needed for rendering this frame
	// will be added when engine.go is implemented
}

type RenderPool struct {
	Workers int
	Jobs    chan FrameJob
	Results chan *image.RGBA
	BufferPool *sync.Pool
}

func NewRenderPool(width, height int) *RenderPool {
	workers := runtime.NumCPU()
	return &RenderPool{
		Workers: workers,
		Jobs:    make(chan FrameJob, workers*2),
		Results: make(chan *image.RGBA, workers*2),
		BufferPool: &sync.Pool{
			New: func() interface{} {
				return image.NewRGBA(image.Rect(0, 0, width, height))
			},
		},
	}
}

// In engine.go we will launch the workers and feed jobs.
