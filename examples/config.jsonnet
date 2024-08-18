{
  rbe_backend: {
    address: '127.0.0.1:8900',
  },
  grpc_server: {
    listen_addresses: [':8905'],
    authentication_policy: { allow: {} },
  },
  push_metrics: {
    push_url: 'http://localhost:8429/api/v1/import/prometheus',
    job_name: 'rbe_meter',
    scrape_interval_ms: 33,
  },
}
