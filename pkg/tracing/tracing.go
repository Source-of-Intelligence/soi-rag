package tracing

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

var tracer trace.Tracer

// InitTracing 初始化OpenTelemetry追踪
func InitTracing(serviceName, collectorURL string) (func(), error) {
	// 创建OTLP exporter
	exporter, err := otlptracegrpc.New(
		context.Background(),
		otlptracegrpc.WithEndpoint(collectorURL),
		otlptracegrpc.WithInsecure(),
	)
	if err != nil {
		return nil, err
	}

	// 创建TracerProvider
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
	)

	// 设置全局TracerProvider
	otel.SetTracerProvider(tp)

	// 创建Tracer
	tracer = tp.Tracer(serviceName)

	// 返回shutdown函数
	shutdown := func() {
		_ = tp.Shutdown(context.Background())
	}

	return shutdown, nil
}

// StartSpan 开始一个span
func StartSpan(ctx context.Context, name string) (context.Context, trace.Span) {
	if tracer == nil {
		return ctx, trace.SpanFromContext(ctx)
	}
	return tracer.Start(ctx, name)
}

// StartSpanWithAttributes 开始一个带属性的span
func StartSpanWithAttributes(ctx context.Context, name string, attrs ...trace.SpanStartOption) (context.Context, trace.Span) {
	if tracer == nil {
		return ctx, trace.SpanFromContext(ctx)
	}
	return tracer.Start(ctx, name, attrs...)
}

// EndSpan 结束span
func EndSpan(span trace.Span, err error) {
	if err != nil {
		span.RecordError(err)
	}
	span.End()
}

// GetTracer 获取全局Tracer
func GetTracer() trace.Tracer {
	return tracer
}

// SetTracer 设置全局Tracer
func SetTracer(t trace.Tracer) {
	tracer = t
}

// SpanFromContext 从context获取当前span
func SpanFromContext(ctx context.Context) trace.Span {
	return trace.SpanFromContext(ctx)
}

// AddEventToSpan 向span添加事件
func AddEventToSpan(ctx context.Context, name string, attrs ...trace.EventOption) {
	span := trace.SpanFromContext(ctx)
	span.AddEvent(name, attrs...)
}

// SetSpanAttributes 设置span属性
func SetSpanAttributes(ctx context.Context, attrs ...attribute.KeyValue) {
	span := trace.SpanFromContext(ctx)
	if span.SpanContext().IsValid() {
		span.SetAttributes(attrs...)
	}
}
