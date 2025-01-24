#!/usr/bin/env bash

port=8010

opts=( "$@" )
#if no argument is passed this for loop will be skipped
for ((i=0;i<$#;i++));
do
  case "${opts[$i]}" in
    --proxyUrl)
      [[ "${opts[$((i+1))]}" != "" ]] &&
      proxyUrl="${opts[$((i+1))]}"
      ((i++))

      ;;

    --port)
      [[ "${opts[$((i+1))]}" != "" ]] &&
      port="${opts[$((i+1))]}"
      ((i++))

      ;;

    --help)
      echo "Usage: devproxy [options]"
      echo "Options:"
      echo "  --proxyUrl <url>  proxy url"
      echo "  --port <port>     port number (default: 8010)"
      exit 0      

      ;;
    *)
      #other unknown options
      echo invalid option ${opts[$((i))]}
      exit 1

      ;;
  esac
done

concurrently --kill-others "lcp --proxyUrl ${proxyUrl} --port ${port}" "export API_SERVER='http://localhost:${port}/proxy' && vite"