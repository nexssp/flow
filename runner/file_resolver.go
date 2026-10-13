package runner

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/nexssp/kernel/xerr"
	"github.com/nexssp/kernel/xfile"
)

type providerFileResolvers map[xfile.Provider][]xfile.Resolver

func newFileResolver(resolvers providerFileResolvers) xfile.Resolver {
	if len(resolvers) == 0 {
		return nil
	}
	return resolvers
}

func (resolvers providerFileResolvers) Open(ctx context.Context, ref xfile.Ref) (io.ReadCloser, xfile.Info, error) {
	providers := resolvers[ref.Provider]
	if len(providers) == 0 {
		return nil, xfile.Info{}, xerr.NotFound(fmt.Sprintf("no file resolver for provider %q", ref.Provider))
	}

	var notFoundErr error
	for _, resolver := range providers {
		reader, info, err := resolver.Open(ctx, ref)
		if err == nil {
			if reader == nil {
				return nil, xfile.Info{}, xerr.Internal("file resolver returned a nil reader")
			}
			if info.Provider != ref.Provider || info.ID != ref.ID {
				if closeErr := reader.Close(); closeErr != nil {
					return nil, xfile.Info{}, errors.Join(
						xerr.Internal("file resolver returned mismatched file identity"),
						fmt.Errorf("close reader: %w", closeErr),
					)
				}
				return nil, xfile.Info{}, xerr.Internal("file resolver returned mismatched file identity")
			}
			return reader, info, nil
		}
		if reader != nil {
			if closeErr := reader.Close(); closeErr != nil {
				return nil, xfile.Info{}, errors.Join(err, fmt.Errorf("close reader after resolver error: %w", closeErr))
			}
		}
		if xerr.KindFrom(err) != xerr.KindNotFound {
			return nil, xfile.Info{}, err
		}
		notFoundErr = err
	}
	if notFoundErr != nil {
		return nil, xfile.Info{}, notFoundErr
	}
	return nil, xfile.Info{}, xerr.NotFound(fmt.Sprintf("file reference %q was not found", ref.ID))
}
