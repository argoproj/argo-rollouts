import { ActionButton } from "argo-ui/v2";
import * as React from 'react';

    export const Timer = (props : { date : string}) => {
    const [time, setTime] = React.useState(0);
    React.useEffect(() => {
       let timer: any;
       let start = Math.floor(((new Date(props.date).getTime()-Date.now())/1000));
       console.log("start is ",start);
        setTime(start);
        timer = setInterval(() => {
           start--;
           if(start === 0 ) {
               clearInterval(timer);
               start = 0;
           }
           setTime(start);
       }, 1000);   
    return () => {
        if(timer) {
            clearInterval(timer);
        }
    }
}, [props.date]);
return  (
   time === 0  ? null: <ActionButton label={`delay: ${time}s`} />)
}
