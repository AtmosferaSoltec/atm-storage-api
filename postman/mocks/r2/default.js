// Local S3 subset based on Cloudflare's documented PutObject/GetObject/DeleteObject.
// Signature validation, bucket credentials and R2 infrastructure are not simulated.
const http = require('http');
http.createServer(async (req, res) => {
  const path = new URL(req.url, 'http://mock').pathname;
  if (path === '/__control/reset' && req.method === 'POST') {
    await pm.state.set('r2.objects', {});
    await pm.state.set('r2.fail', '');
    res.writeHead(204); return res.end();
  }
  if (path.startsWith('/__control/fail/') && req.method === 'POST') {
    await pm.state.set('r2.fail', path.split('/').pop());
    res.writeHead(204); return res.end();
  }
  if (!path.startsWith('/atm-storage-dev/')) { res.writeHead(501); return res.end(); }
  if ((await pm.state.get('r2.fail')) === req.method) {
    res.writeHead(503, {'Content-Type': 'application/xml'});
    return res.end('<Error><Code>ServiceUnavailable</Code><Message>Simulated outage</Message></Error>');
  }
  const objects = (await pm.state.get('r2.objects')) || {};
  if (req.method === 'PUT') {
    const body = Buffer.isBuffer(req.body) ? req.body : Buffer.from(typeof req.body === 'string' ? req.body : '');
    objects[path] = {body: body.toString('base64'), contentType: req.headers['content-type']};
    await pm.state.set('r2.objects', objects);
    res.writeHead(200, {ETag: '"mock-etag"'}); return res.end();
  }
  if (req.method === 'GET') {
    if (!objects[path]) { res.writeHead(404, {'Content-Type': 'application/xml'}); return res.end('<Error><Code>NoSuchKey</Code></Error>'); }
    res.writeHead(200, {'Content-Type': objects[path].contentType});
    return res.end(Buffer.from(objects[path].body, 'base64'));
  }
  if (req.method === 'DELETE') {
    delete objects[path]; await pm.state.set('r2.objects', objects);
    res.writeHead(204); return res.end();
  }
  res.writeHead(501); res.end();
}).listen(process.env.PORT);
