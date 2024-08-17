{
  rbe_backend: {
    address: '127.0.0.1:8900',
  },
  grpc_server: {
    listen_addresses: [':12000'],
    authentication_policy: { allow: {} },
  },
}
