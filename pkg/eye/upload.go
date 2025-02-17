package eye

import (
	"context"
	"net/url"
	"time"

	"github.com/gogo/protobuf/proto"
	"github.com/golang/snappy"
	"github.com/pkg/errors"
	prometheusconfig "github.com/prometheus/common/config"
	"github.com/prometheus/common/expfmt"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/prompb"
	"github.com/prometheus/prometheus/storage/remote"
	"go.uber.org/zap"

	"github.com/outofforest/cloudless"
	"github.com/outofforest/cloudless/pkg/host"
	"github.com/outofforest/logger"
	"github.com/outofforest/parallel"
)

const (
	uploadInterval = 15 * time.Second
	timeout        = time.Second
)

// UploadService returns new prometheus metric uploader service.
func UploadService(prometheusURL string) host.Configurator {
	var c host.SealedConfiguration
	return cloudless.Join(
		cloudless.Configuration(&c),
		cloudless.Service("eye", parallel.Fail, func(ctx context.Context) error {
			log := logger.Get(ctx)
			standardLabels := []prompb.Label{
				{
					Name:  "box",
					Value: c.Hostname(),
				},
			}
			gatherer := c.MetricGatherer()
			pURL, err := url.Parse(prometheusURL + "/api/v1/write")
			if err != nil {
				return errors.WithStack(err)
			}

			client, err := remote.NewWriteClient("metrics", &remote.ClientConfig{
				URL:     &prometheusconfig.URL{URL: pURL},
				Timeout: model.Duration(timeout),
			})
			if err != nil {
				return errors.WithStack(err)
			}

			timer := time.NewTicker(uploadInterval)
			defer timer.Stop()

			for {
				select {
				case <-ctx.Done():
					return errors.WithStack(ctx.Err())
				case <-timer.C:
				}

				err := func() error {
					mfs, err := gatherer.Gather()
					if err != nil {
						return errors.WithStack(err)
					}
					if len(mfs) == 0 {
						return nil
					}

					samples, err := expfmt.ExtractSamples(&expfmt.DecodeOptions{
						Timestamp: model.Now(),
					}, mfs...)
					if err != nil {
						return errors.WithStack(err)
					}

					buf, err := encodePrometheusSamples(samples, standardLabels)
					if err != nil {
						return errors.WithStack(err)
					}
					if _, err := client.Store(ctx, buf, 0); err != nil {
						return errors.WithStack(errors.WithStack(err))
					}

					return nil
				}()

				if err != nil {
					if errors.Is(err, ctx.Err()) {
						return errors.WithStack(err)
					}
					log.Error("Sending metrics failed", zap.Error(err))
				}
			}
		}),
	)
}

func encodePrometheusSamples(samples []*model.Sample, standardLabels []prompb.Label) ([]byte, error) {
	req := &prompb.WriteRequest{
		Timeseries: make([]prompb.TimeSeries, 0, len(samples)),
	}

	for _, s := range samples {
		ts := prompb.TimeSeries{
			Labels: append(toLabelPairs(s.Metric), standardLabels...),
			Samples: []prompb.Sample{
				{
					Value:     float64(s.Value),
					Timestamp: int64(s.Timestamp),
				},
			},
		}
		req.Timeseries = append(req.Timeseries, ts)
	}

	pBuf := proto.NewBuffer(nil)

	if err := pBuf.Marshal(req); err != nil {
		return nil, errors.WithStack(err)
	}

	return snappy.Encode(nil, pBuf.Bytes()), nil
}

func toLabelPairs(metric model.Metric) []prompb.Label {
	labelPairs := make([]prompb.Label, 0, len(metric))
	for k, v := range metric {
		labelPairs = append(labelPairs, prompb.Label{
			Name:  string(k),
			Value: string(v),
		})
	}
	return labelPairs
}
